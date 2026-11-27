package repo

import (
	"errors"
	"testing"

	"agentflow/internal/domain"
)

func TestUserProfileDisplayNamePreservedAcrossLoginRefresh(t *testing.T) {
	st, err := OpenTest()
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	if err := st.PutUser(domain.User{LoginName: "alice", ChineseName: "爱丽丝"}); err != nil {
		t.Fatal(err)
	}
	user, ok := st.UserByLoginName("alice")
	if !ok {
		t.Fatal("user not found")
	}
	if user.DisplayName != "" || user.PasswordLogin || user.AvatarConfigured || user.AvatarVersion != 0 {
		t.Fatalf("unexpected default profile: %+v", user)
	}
	if err := st.UpdateDisplayName(user.ID, "  Alice W  "); err != nil {
		t.Fatal(err)
	}
	user, _ = st.UserByLoginName("alice")
	if user.DisplayName != "Alice W" {
		t.Fatalf("display name = %q", user.DisplayName)
	}

	// An authentication refresh has no display name; it must keep the override.
	if err := st.PutUser(domain.User{LoginName: "alice", ChineseName: "爱丽丝"}); err != nil {
		t.Fatal(err)
	}
	user, _ = st.UserByLoginName("alice")
	if user.DisplayName != "Alice W" || user.ChineseName != "爱丽丝" {
		t.Fatalf("login refresh lost profile: %+v", user)
	}

	if err := st.UpdateDisplayName(user.ID, "   "); err != nil {
		t.Fatal(err)
	}
	user, _ = st.UserByLoginName("alice")
	if user.DisplayName != "" {
		t.Fatalf("empty name should clear the override, got %q", user.DisplayName)
	}

	if err := st.UpdateDisplayName(999999, "nobody"); !errors.Is(err, ErrUserNotFound) {
		t.Fatalf("unknown user: %v", err)
	}
}

func TestUserAvatarLifecycle(t *testing.T) {
	st, err := OpenTest()
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	if err := st.PutUser(domain.User{LoginName: "avatar-user"}); err != nil {
		t.Fatal(err)
	}
	user, _ := st.UserByLoginName("avatar-user")
	if _, ok := st.UserAvatar(user.ID); ok {
		t.Fatal("new user must not have an avatar")
	}
	if _, err := st.PutUserAvatar(999999, "image/png", []byte("x")); !errors.Is(err, ErrUserNotFound) {
		t.Fatalf("unknown user: %v", err)
	}

	first, err := st.PutUserAvatar(user.ID, "image/png", []byte("first"))
	if err != nil {
		t.Fatal(err)
	}
	avatar, ok := st.UserAvatar(user.ID)
	if !ok || avatar.ContentType != "image/png" || string(avatar.Data) != "first" || avatar.Version != first {
		t.Fatalf("avatar = %+v", avatar)
	}
	user, _ = st.User(user.ID)
	if !user.AvatarConfigured || user.AvatarVersion != first {
		t.Fatalf("user avatar flags = %+v", user)
	}

	second, err := st.PutUserAvatar(user.ID, "image/jpeg", []byte("second"))
	if err != nil {
		t.Fatal(err)
	}
	if second <= first {
		t.Fatalf("version must advance: %d -> %d", first, second)
	}
	avatar, _ = st.UserAvatar(user.ID)
	if string(avatar.Data) != "second" || avatar.ContentType != "image/jpeg" {
		t.Fatalf("replacement = %+v", avatar)
	}

	if err := st.DeleteUserAvatar(user.ID); err != nil {
		t.Fatal(err)
	}
	if _, ok := st.UserAvatar(user.ID); ok {
		t.Fatal("avatar should be gone")
	}
	user, _ = st.User(user.ID)
	if user.AvatarConfigured || user.AvatarVersion != 0 {
		t.Fatalf("avatar flags should reset: %+v", user)
	}
	// Deleting again is a no-op.
	if err := st.DeleteUserAvatar(user.ID); err != nil {
		t.Fatal(err)
	}
}

func TestChangeLocalPassword(t *testing.T) {
	st, err := OpenTest()
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	if _, err := st.CreateLocalAccount("bob", "old-password", false); err != nil {
		t.Fatal(err)
	}
	user, _ := st.UserByLoginName("bob")
	if !user.PasswordLogin || !st.HasLocalPassword(user.ID) {
		t.Fatal("created account must report a password login")
	}
	if err := st.ChangeLocalPassword(user.ID, "wrong-password", "new-password"); !errors.Is(err, ErrInvalidCurrentPassword) {
		t.Fatalf("wrong current password: %v", err)
	}
	if !st.CheckLocalPassword(user.ID, "old-password") {
		t.Fatal("failed change must keep the old password")
	}
	if err := st.ChangeLocalPassword(user.ID, "old-password", "new-password"); err != nil {
		t.Fatal(err)
	}
	if st.CheckLocalPassword(user.ID, "old-password") || !st.CheckLocalPassword(user.ID, "new-password") {
		t.Fatal("password did not change")
	}

	if err := st.PutUser(domain.User{LoginName: "sso-user"}); err != nil {
		t.Fatal(err)
	}
	sso, _ := st.UserByLoginName("sso-user")
	if err := st.ChangeLocalPassword(sso.ID, "any", "new-password"); !errors.Is(err, ErrNoLocalPassword) {
		t.Fatalf("account without a password: %v", err)
	}
}

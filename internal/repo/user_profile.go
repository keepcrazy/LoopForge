package repo

import (
	"errors"
	"strings"
	"time"
)

// ErrUserNotFound is returned when a profile write targets an unknown user.
var ErrUserNotFound = errors.New("user not found")

// UserAvatar is a user-uploaded avatar image. Version changes on every
// upload so profile URLs can be cached forever.
type UserAvatar struct {
	ContentType string
	Data        []byte
	Version     int64
}

// UpdateDisplayName sets the user's own display name. An empty name clears
// the override, so the directory / SSO name shows again.
func (r *UsersRepo) UpdateDisplayName(userID int64, name string) error {
	name = strings.TrimSpace(name)
	result := r.DB.Model(&userRow{}).Where("id = ?", userID).Updates(map[string]any{
		"display_name": name,
		"updated_at":   time.Now().UTC(),
	})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		if _, ok := r.User(userID); !ok {
			return ErrUserNotFound
		}
	}
	return nil
}

// UserAvatar returns the uploaded avatar for a user, if any.
func (r *UsersRepo) UserAvatar(userID int64) (UserAvatar, bool) {
	if userID <= 0 {
		return UserAvatar{}, false
	}
	var row userAvatarRow
	if err := r.DB.Where("user_id = ?", userID).Take(&row).Error; err != nil {
		return UserAvatar{}, false
	}
	return UserAvatar{ContentType: row.ContentType, Data: row.Data, Version: row.Version}, true
}

// PutUserAvatar stores a user's avatar and returns its new cache version.
// Callers must validate the image type and size before calling this.
func (r *UsersRepo) PutUserAvatar(userID int64, contentType string, data []byte) (int64, error) {
	if userID <= 0 {
		return 0, ErrUserNotFound
	}
	if _, ok := r.User(userID); !ok {
		return 0, ErrUserNotFound
	}
	if contentType == "" || len(data) == 0 {
		return 0, errors.New("empty avatar")
	}
	version := time.Now().UTC().UnixMilli()
	if current, ok := r.UserAvatar(userID); ok && version <= current.Version {
		version = current.Version + 1
	}
	err := upsert(r.DB, "user_avatars", map[string]any{
		"user_id":      userID,
		"content_type": contentType,
		"data":         data,
		"version":      version,
		"updated_at":   time.Now().UTC(),
	})
	if err != nil {
		return 0, err
	}
	return version, nil
}

// DeleteUserAvatar removes a user's uploaded avatar, falling back to the
// system / SSO photo. Removing a missing avatar is not an error.
func (r *UsersRepo) DeleteUserAvatar(userID int64) error {
	if userID <= 0 {
		return ErrUserNotFound
	}
	return r.DB.Where("user_id = ?", userID).Delete(&userAvatarRow{}).Error
}

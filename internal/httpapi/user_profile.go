package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"agentflow/internal/auth"
	"agentflow/internal/domain"
	"agentflow/internal/repo"

	"github.com/gin-gonic/gin"
)

const (
	// MaxAvatarBytes bounds an uploaded avatar. The settings page crops the
	// image to a 512x512 JPEG before upload, so this is generous.
	MaxAvatarBytes      = 2 << 20
	maxDisplayNameRunes = 40
	maxAvatarSide       = 4096
	minAvatarSide       = 16
	avatarCacheControl  = "public, max-age=31536000, immutable"
)

func validBrowserOrigin(c *gin.Context) bool {
	origin, err := url.Parse(c.GetHeader("Origin"))
	return err == nil && origin.Host == c.Request.Host &&
		(origin.Scheme == "http" || origin.Scheme == "https")
}

// requireSelf admits the signed-in owner of :id. Browser calls must also be
// same-origin; bearer-token clients are exempt because they never carry the
// session cookie.
func (h *usersHandlers) requireSelf(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, map[string]string{"error": "valid user id is required"})
		return 0, false
	}
	current, ok := h.requestUser(c.Request, true)
	if !ok || current == nil || current.IsGuest || current.ID <= 0 {
		c.JSON(http.StatusUnauthorized, map[string]string{"error": "请先登录后再修改个人资料"})
		return 0, false
	}
	if current.ID != id {
		c.JSON(http.StatusForbidden, map[string]string{"error": "只能修改自己的个人资料"})
		return 0, false
	}
	if _, bearer := bearerToken(c.Request); !bearer && !validBrowserOrigin(c) {
		c.JSON(http.StatusForbidden, map[string]string{"error": "invalid request origin"})
		return 0, false
	}
	return id, true
}

func validDisplayName(name string) error {
	if name == "" {
		return nil
	}
	if utf8.RuneCountInString(name) > maxDisplayNameRunes {
		return fmt.Errorf("显示名称最多 %d 个字符", maxDisplayNameRunes)
	}
	for _, r := range name {
		if unicode.IsControl(r) {
			return errors.New("显示名称不能包含换行或控制字符")
		}
	}
	return nil
}

// updateUserProfile changes the user's own display name. An empty name
// removes the override and shows the directory / SSO name again.
func (h *usersHandlers) updateUserProfile(c *gin.Context) {
	id, ok := h.requireSelf(c)
	if !ok {
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 4096)
	var input struct {
		DisplayName string `json:"displayName"`
	}
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid profile request"})
		return
	}
	name := strings.TrimSpace(input.DisplayName)
	if err := validDisplayName(name); err != nil {
		c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if err := h.repo.UpdateDisplayName(id, name); err != nil {
		if errors.Is(err, repo.ErrUserNotFound) {
			c.JSON(http.StatusNotFound, map[string]string{"error": "用户不存在"})
			return
		}
		respond(c, err)
		return
	}
	saved, found := h.repo.User(id)
	if !found {
		c.JSON(http.StatusNotFound, map[string]string{"error": "用户不存在"})
		return
	}
	c.JSON(http.StatusOK, domain.PublicUser(saved))
}

// updateUserAvatar accepts a raw image body (Content-Type is sniffed, not
// trusted) and stores it as the user's avatar. Settings crops it to a square
// before upload; the endpoint still validates type, size and dimensions.
func (h *usersHandlers) updateUserAvatar(c *gin.Context) {
	id, ok := h.requireSelf(c)
	if !ok {
		return
	}
	reader := http.MaxBytesReader(c.Writer, c.Request.Body, MaxAvatarBytes)
	data, err := io.ReadAll(reader)
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			c.JSON(http.StatusRequestEntityTooLarge, map[string]string{"error": fmt.Sprintf("头像文件不能超过 %d MB", MaxAvatarBytes>>20)})
			return
		}
		c.JSON(http.StatusBadRequest, map[string]string{"error": "无法读取头像文件"})
		return
	}
	contentType, err := avatarContentType(data)
	if err != nil {
		c.JSON(http.StatusUnsupportedMediaType, map[string]string{"error": err.Error()})
		return
	}
	version, err := h.repo.PutUserAvatar(id, contentType, data)
	if err != nil {
		if errors.Is(err, repo.ErrUserNotFound) {
			c.JSON(http.StatusNotFound, map[string]string{"error": "用户不存在"})
			return
		}
		respond(c, err)
		return
	}
	saved, found := h.repo.User(id)
	if !found {
		c.JSON(http.StatusNotFound, map[string]string{"error": "用户不存在"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"user":             domain.PublicUser(saved),
		"avatarUrl":        avatarURLForDomainUser(saved),
		"avatarVersion":    version,
		"avatarConfigured": true,
	})
}

// deleteUserAvatar removes the uploaded avatar and restores the system / SSO
// photo. Removing one that is already gone is a no-op.
func (h *usersHandlers) deleteUserAvatar(c *gin.Context) {
	id, ok := h.requireSelf(c)
	if !ok {
		return
	}
	if err := h.repo.DeleteUserAvatar(id); err != nil {
		respond(c, err)
		return
	}
	saved, found := h.repo.User(id)
	if !found {
		c.JSON(http.StatusNotFound, map[string]string{"error": "用户不存在"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"user":             domain.PublicUser(saved),
		"avatarUrl":        avatarURLForDomainUser(saved),
		"avatarConfigured": false,
		"avatarVersion":    0,
	})
}

// avatarContentType sniffs the image, rejects anything but a still JPEG/PNG
// and bounds its pixel dimensions.
func avatarContentType(data []byte) (string, error) {
	contentType := http.DetectContentType(data)
	switch contentType {
	case "image/jpeg", "image/png":
	default:
		return "", errors.New("头像仅支持 JPG 或 PNG 图片")
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return "", errors.New("头像文件无法解析，请换一张图片")
	}
	if cfg.Width < minAvatarSide || cfg.Height < minAvatarSide {
		return "", fmt.Errorf("头像尺寸至少 %d×%d 像素", minAvatarSide, minAvatarSide)
	}
	if cfg.Width > maxAvatarSide || cfg.Height > maxAvatarSide {
		return "", fmt.Errorf("头像尺寸最大 %d×%d 像素", maxAvatarSide, maxAvatarSide)
	}
	return contentType, nil
}

// avatarURLForDomainUser is the relative URL all avatar renderers use. A
// custom avatar carries its version so it can be cached forever.
func avatarURLForDomainUser(u domain.User) string {
	raw := auth.AvatarURL(u.LoginName, 100)
	if raw == "" || !u.AvatarConfigured || u.AvatarVersion <= 0 {
		return raw
	}
	return fmt.Sprintf("%s?v=%d", raw, u.AvatarVersion)
}

func avatarURLForAuth(u *auth.User) string {
	if u == nil {
		return ""
	}
	if raw := auth.AvatarURL(u.LoginName, 100); raw != "" && u.AvatarConfigured && u.AvatarVersion > 0 {
		return fmt.Sprintf("%s?v=%d", raw, u.AvatarVersion)
	}
	return auth.AvatarURL(u.LoginName, 100)
}

// applyStoredProfile copies profile fields the signed cookie does not carry
// (they change without re-issuing the session).
func applyStoredProfile(u *auth.User, stored domain.User) {
	if u == nil {
		return
	}
	u.ID = stored.ID
	u.StaffID = stored.StaffID
	u.ChineseName = stored.ChineseName
	u.DisplayName = stored.DisplayName
	u.DeptID = stored.DeptID
	u.DeptName = stored.DeptName
	u.PasswordLogin = stored.PasswordLogin
	u.AvatarConfigured = stored.AvatarConfigured
	u.AvatarVersion = stored.AvatarVersion
	u.AvatarURL = avatarURLForAuth(u)
}

package httpapi

import (
	"errors"
	"fmt"
	"net/http"

	"agentflow/internal/repo"

	"github.com/gin-gonic/gin"
)

// authChangePassword lets a local password account replace its own password
// after proving it knows the current one. Accounts that sign in through
// GitHub or a gateway have no platform password and are refused.
func (h *authHandlers) authChangePassword(c *gin.Context) {
	if !validBrowserOrigin(c) {
		c.JSON(http.StatusForbidden, gin.H{"error": "invalid request origin"})
		return
	}
	user, ok := h.requestUser(c.Request, false)
	if !ok || user == nil || user.IsGuest || user.ID <= 0 {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "请先登录后再修改密码"})
		return
	}
	if !h.repo.HasLocalPassword(user.ID) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "当前账号通过 GitHub 或单点登录，密码由身份系统管理"})
		return
	}
	if !h.allowPasswordAttempt(c.RemoteIP()) {
		c.JSON(http.StatusTooManyRequests, gin.H{"error": "Too many attempts. Try again in a minute."})
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 4096)
	var input struct {
		CurrentPassword string `json:"currentPassword"`
		NewPassword     string `json:"newPassword"`
	}
	if c.ShouldBindJSON(&input) != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid password request"})
		return
	}
	if len(input.NewPassword) < repo.MinPasswordLength || len(input.NewPassword) > repo.MaxPasswordLength {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("密码需 %d–%d 个字符", repo.MinPasswordLength, repo.MaxPasswordLength)})
		return
	}
	if err := h.repo.ChangeLocalPassword(user.ID, input.CurrentPassword, input.NewPassword); err != nil {
		switch {
		case errors.Is(err, repo.ErrInvalidCurrentPassword):
			c.JSON(http.StatusBadRequest, gin.H{"error": "当前密码不正确"})
		case errors.Is(err, repo.ErrNoLocalPassword):
			c.JSON(http.StatusBadRequest, gin.H{"error": "当前账号没有平台密码"})
		default:
			respond(c, err)
		}
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

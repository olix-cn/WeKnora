package middleware

import (
	"errors"
	"net/http"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/application/repository"
	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/gin-gonic/gin"
)

// RequireKBMemberOrOwnership lets explicit KB editor/admin permissions satisfy
// the corresponding resource gate without promoting the caller's workspace
// role. Unmanaged legacy KBs still use the existing ownership guard.
func RequireKBMemberOrOwnership(
	resolve KBIDResolver, required types.OrgMemberRole, kbs KBLookup,
	shares access.KBShareLookup, legacy gin.HandlerFunc,
) gin.HandlerFunc {
	return func(c *gin.Context) {
		lookup := access.MemberLookup(shares)
		if lookup == nil {
			legacy(c)
			return
		}
		id, err := resolve(c)
		if err != nil {
			_ = c.Error(err)
			c.Abort()
			return
		}
		kb, err := kbs.GetKnowledgeBaseByID(c.Request.Context(), id)
		if err != nil || kb == nil {
			if errors.Is(err, repository.ErrKnowledgeBaseNotFound) || (err == nil && kb == nil) {
				_ = c.Error(apperrors.NewNotFoundError("knowledge base not found"))
			} else {
				_ = c.Error(apperrors.NewServiceUnavailableError("cannot verify knowledge base member access"))
			}
			c.Abort()
			return
		}
		permission, handled, err := lookup.CheckKBMemberAccess(
			c.Request.Context(), kb.ID, kb.TenantID, types.CallerFromContext(c.Request.Context()))
		if err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "cannot verify knowledge base member access"})
			c.Abort()
			return
		}
		if !handled {
			legacy(c)
			return
		}
		if !required.IsValid() || !permission.IsValid() || !permission.HasPermission(required) {
			c.JSON(http.StatusForbidden, gin.H{"error": "insufficient knowledge base permission"})
			c.Abort()
			return
		}
		c.Next()
	}
}

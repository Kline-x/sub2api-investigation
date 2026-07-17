package admin

import (
	"errors"
	"fmt"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/xai"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

// convertXaiAccount 把 GROK CPA 导出的 xai-*.json 单账号对象转成 DataAccount。
// 定制导入路径:
//   - 从 access_token JWT payload 解析 client_id/team_id/scope 等(不验签)
//   - email 取字段,缺省用 JWT claim,再缺省用 grok-import-<序号>
//   - base_url 固定 CLI 代理端点(与 BuildAccountCredentials 一致)
func convertXaiAccount(raw map[string]any, index int) (DataAccount, error) {
	str := func(key string) string {
		v, _ := raw[key].(string)
		return strings.TrimSpace(v)
	}

	accessToken := str("access_token")
	if accessToken == "" {
		return DataAccount{}, errors.New("xai account missing access_token")
	}

	claims := xai.DecodeJWTClaims(accessToken)
	claim := func(key string) string {
		if claims == nil {
			return ""
		}
		return xai.JWTClaimString(claims, key)
	}

	email := str("email")
	if email == "" {
		email = claim("email")
	}
	if email == "" {
		email = fmt.Sprintf("grok-import-%d", index+1)
	}

	sub := str("sub")
	if sub == "" {
		sub = claim("sub")
	}
	tokenType := str("token_type")
	if tokenType == "" {
		tokenType = "Bearer"
	}

	return DataAccount{
		Name:     email,
		Platform: service.PlatformGrok,
		Type:     service.AccountTypeOAuth,
		Credentials: map[string]any{
			"access_token":  accessToken,
			"refresh_token": str("refresh_token"),
			"id_token":      str("id_token"),
			"token_type":    tokenType,
			"client_id":     claim("client_id"),
			"team_id":       claim("team_id"),
			"scope":         claim("scope"),
			"email":         email,
			"sub":           sub,
			"expires_at":    str("expired"),
			"base_url":      xai.DefaultCLIBaseURL,
		},
		Concurrency: 1,
		Priority:    0,
	}, nil
}
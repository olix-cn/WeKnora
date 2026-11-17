package documentgen

import (
	"strings"
	"time"
	"unicode"

	"github.com/Tencent/WeKnora/internal/common"
)

// AssemblyFileName uses the root identity and task creation time, not a child
// part, template name, model argument or caller-supplied filesystem path.
func AssemblyFileName(root AssemblyIdentity, created time.Time) string {
	clean := func(value, fallback string) string {
		value = strings.Map(func(r rune) rune {
			if unicode.IsControl(r) || strings.ContainsRune(`<>:"/\|?*`, r) {
				return '_'
			}
			return r
		}, value)
		value = strings.Trim(value, " .\t\r\n")
		value = common.TruncateUTF8Bytes(value, 90)
		value = strings.Trim(value, " .")
		if value == "" {
			return fallback
		}
		return value
	}
	beijing := time.FixedZone("Asia/Shanghai", 8*60*60)
	return clean(root.Number, "未知图号") + "-" + clean(root.Name, "未命名总装配") + "-" +
		created.In(beijing).Format("20060102150405") + ".xlsx"
}

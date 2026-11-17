package documentgen

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/stretchr/testify/require"
)

func TestAssemblyFileNameUsesBeijingTaskTime(t *testing.T) {
	created := time.Date(2026, 11, 12, 6, 30, 25, 0, time.UTC)
	require.Equal(t, "009S11-左前减振器芯子总成-20261112143025.xlsx",
		AssemblyFileName(AssemblyIdentity{Number: "009S11", Name: "左前减振器芯子总成"}, created))
	created = time.Date(2026, 11, 12, 20, 1, 2, 0, time.UTC)
	require.Equal(t, "ROOT-总成-20261113040102.xlsx",
		AssemblyFileName(AssemblyIdentity{Number: "ROOT", Name: "总成"}, created))
}

func TestAssemblyFileNameBoundsAndNeutralizesUnsafeCharacters(t *testing.T) {
	now := time.Date(2026, 11, 12, 0, 0, 0, 0, time.UTC)
	name := AssemblyFileName(AssemblyIdentity{Number: `../A:B`, Name: "装配/图\\纸\n\"<>|?*"}, now)
	require.NotContains(t, name, "/")
	require.NotContains(t, name, "\\")
	require.NotContains(t, name, "\n")
	require.NotContains(t, name, ":")
	require.NotContains(t, name, `"`)
	name = AssemblyFileName(AssemblyIdentity{Number: strings.Repeat("号", 200), Name: strings.Repeat("装", 200)}, now)
	require.True(t, utf8.ValidString(name))
	require.Less(t, len(name), 220)
	require.Equal(t, "未知图号-未命名总装配-20261112080000.xlsx", AssemblyFileName(AssemblyIdentity{}, now))
}

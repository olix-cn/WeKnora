package types

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRefreshLegacyPromptIdentity(t *testing.T) {
	const suffix = ". Explain the source WeKnora and 腾讯 without renaming it.\r\n"
	for _, intro := range legacyPromptIdentities {
		t.Run(intro, func(t *testing.T) {
			want := strings.TrimSuffix(strings.Replace(intro, "WeKnora", "YHSJ", 1), " developed by Tencent")
			got := RefreshLegacyPromptIdentity("\r\n " + intro + suffix)
			require.Equal(t, "\r\n "+want+suffix, got)
			require.Equal(t, got, RefreshLegacyPromptIdentity(got))
		})
	}
	require.Equal(t, "You are YHSJ, an assistant, specializing in lease and sales questions.",
		RefreshLegacyPromptIdentity("You are WeKnora, an assistant developed by Tencent, specializing in lease and sales questions."))
	require.Equal(t, "You are YHSJ, an intelligent assistant that cites sources.",
		RefreshLegacyPromptIdentity("You are WeKnora, an intelligent assistant developed by Tencent that cites sources."))
	require.Equal(t, "<role>\r\nYou are the YHSJ Skill Installer.\r\n</role>\r\nUse `.weknora/requirements.json`.",
		RefreshLegacyPromptIdentity("<role>\r\nYou are the WeKnora Skill Installer.\r\n</role>\r\nUse `.weknora/requirements.json`."))
	for _, prompt := range []string{
		"", "You are Tencent's public earnings analyst.", "You are WeKnora Data Analyst Pro.",
		"Quote this example without changes: \nYou are WeKnora, an assistant developed by Tencent.",
		"You are WeKnora, an assistant for users of our software.",
		"You are WeKnora, an assistant developed by Tencent Cloud Labs.",
	} {
		require.Equal(t, prompt, RefreshLegacyPromptIdentity(prompt))
	}
}

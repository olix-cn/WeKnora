package types

import "strings"

// RefreshLegacyPromptIdentity updates only a known, stock self-introduction at
// the start of a saved system prompt. Legacy agents could save copies of built-in
// templates. Preserve their instructions and references, and never apply this
// to rendered source content, history, questions or answers.
func RefreshLegacyPromptIdentity(prompt string) string {
	body := strings.TrimLeft(prompt, " \t\r\n")
	// The installer template places its self-introduction inside <role>.
	if rest, ok := strings.CutPrefix(body, "<role>"); ok {
		body = strings.TrimLeft(rest, " \t\r\n")
	}
	for _, identity := range legacyPromptIdentities {
		rest, ok := strings.CutPrefix(body, identity)
		if !ok || !hasIdentityBoundary(rest) {
			continue
		}
		updated := strings.Replace(identity, "WeKnora", "YHSJ", 1)
		updated = strings.TrimSuffix(updated, " developed by Tencent")
		return prompt[:len(prompt)-len(body)] + updated + rest
	}
	return prompt
}

func hasIdentityBoundary(rest string) bool {
	if rest == "" || strings.HasPrefix(rest, " that ") {
		return true
	}
	switch rest[0] {
	case '.', ',', '\r', '\n':
		return true
	default:
		return false
	}
}

var legacyPromptIdentities = [...]string{
	"You are WeKnora Data Analyst",
	"You are WeKnora Wiki Fixer",
	"You are WeKnora Wiki Researcher",
	"You are WeKnora, a knowledge-base question-answering assistant developed by Tencent",
	"You are WeKnora, a professional and friendly AI assistant developed by Tencent",
	"You are WeKnora, a professional and friendly customer service assistant developed by Tencent",
	"You are WeKnora, a professional and friendly intelligent assistant developed by Tencent",
	"You are WeKnora, a professional intelligent assistant developed by Tencent",
	"You are WeKnora, a professional technical support engineer developed by Tencent",
	"You are WeKnora, a senior domain expert assistant developed by Tencent",
	"You are WeKnora, an assistant",
	"You are WeKnora, an assistant developed by Tencent",
	"You are WeKnora, an intelligent assistant developed by Tencent",
	"You are WeKnora, an intelligent conversational assistant developed by Tencent",
	"You are the WeKnora Assistant",
	"You are the WeKnora Skill Installer",
}

// AssistantBrandPrompt applies to the product identity independently of the
// active role, source material and model provider.
const AssistantBrandPrompt = `The product the user is interacting with is YHSJ. ` +
	`If your identity is relevant, identify yourself as a YHSJ assistant and describe your assigned role. ` +
	`A configured assistant name is a display name within YHSJ, not a different product identity. ` +
	`Do not infer a developer or model provider from the product name or reuse a prior self-introduction from conversation history. ` +
	`When the subject of the question calls for it, preserve real third-party names and source attributions.`

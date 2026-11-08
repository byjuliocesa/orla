package cli

import "testing"

const contextMD = "## Context Usage\n\n**Model:** claude-opus-5-5  \n**Tokens:** 774.9k / 1m (77%)\n\n### Estimated usage by category\n\n| Category | Tokens | Percentage |\n|----------|--------|------------|\n| System prompt | 3k | 0.3% |\n| Messages | 760.1k | 76.0% |\n| Free space | 192.1k | 19.2% |\n\n### MCP Tools\n\n| Tool | Server | Tokens |\n|------|--------|--------|\n| mcp__x__batch | x | 162 |\n\n### Memory Files\n\n| Type | Path | Tokens |\n|------|------|--------|\n| AutoMem | /m/MEMORY.md | 82 |\n\n### Skills\n\n| Skill | Source | Tokens |\n|-------|--------|--------|\n| impeccable | User | ~300 |\n"

func TestParseContext(t *testing.T) {
	u := parseContext(contextMD)
	if u.Model != "claude-opus-5-5" || u.Used != 774900 || u.Total != 1000000 {
		t.Fatalf("header = %+v", u)
	}
	if len(u.Categories) != 3 || u.Categories[1].Name != "Messages" || u.Categories[1].Tokens != 760100 || u.Categories[1].Percent != 76 {
		t.Fatalf("categories = %+v", u.Categories)
	}
	if len(u.MCPTools) != 1 || u.MCPTools[0].Tokens != 162 || u.MCPTools[0].Detail != "x" {
		t.Fatalf("mcp = %+v", u.MCPTools)
	}
	if len(u.Skills) != 1 || u.Skills[0].Tokens != 300 || len(u.Memory) != 1 || u.Memory[0].Name != "/m/MEMORY.md" {
		t.Fatalf("skills/memory = %+v %+v", u.Skills, u.Memory)
	}
}

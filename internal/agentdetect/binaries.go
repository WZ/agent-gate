package agentdetect

// KnownBinaries is the conservative list of agent binaries we look up via $PATH.
// Add new entries here as agents emerge; do not glob the filesystem.
var KnownBinaries = []string{
	"claude",   // Claude Code
	"codex",    // OpenAI Codex CLI
	"aider",    // aider.chat
	"opencode", // openclaw / opencode
}

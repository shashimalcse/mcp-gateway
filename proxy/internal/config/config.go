package config

// Unprotected, when true, disables JWT authentication and scope checks for local/dev.
// NEVER enable in production.
var Unprotected bool = false

// AllowedOrigins lists accepted Origin values for MCP requests. Empty means allow all in Unprotected mode,
// and deny if not matched in protected mode.
var AllowedOrigins []string

// Supported protocol versions (latest + fallback)
const MCPProtocolVersionLatest = "2025-06-18"
const MCPProtocolVersionFallback = "2025-03-26"

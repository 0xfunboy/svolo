package server

import "svolo.local/core/internal/agent"

// BuiltinCatalog describes local tools without opening a browser, reading user
// configuration, or starting an external runtime. A fresh schema is returned.
func BuiltinCatalog() []agent.Tool { return builtins() }

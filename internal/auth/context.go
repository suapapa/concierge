package auth

// Context keys for Gin (string keys to avoid import cycles with gin types in helpers).
const (
	CtxUserIDKey = "conciergeUserID"
	CtxRoleKey   = "conciergeRole"
	CtxLegacyKey = "conciergeLegacyBearer"
)

// RoleAdmin and RoleGuest match the database CHECK constraint.
const (
	RoleAdmin = "admin"
	RoleGuest = "guest"
)

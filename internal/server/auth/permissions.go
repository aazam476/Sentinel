// SPDX-License-Identifier: Apache-2.0

package auth

// Role is a user's role. Each role can do everything the role before it can.
type Role string

// The roles, from least to most powerful.
const (
	RoleViewer   Role = "viewer"
	RoleOperator Role = "operator"
	RoleAdmin    Role = "admin"
)

// Permission is something a user may or may not do.
type Permission string

// Every permission checked anywhere in the server.
const (
	// Viewer: see dashboards, metrics, inventory, storage and logs.
	PermissionViewFleet Permission = "view_fleet"

	// Operator: act on machines.
	PermissionUseTerminal    Permission = "use_terminal"
	PermissionUseDesktop     Permission = "use_desktop"
	PermissionManageFiles    Permission = "manage_files"
	PermissionManageServices Permission = "manage_services"
	// PermissionManageProcesses covers seeing processes too: their command
	// lines can hold secrets that a viewer shouldn't see.
	PermissionManageProcesses Permission = "manage_processes"
	PermissionControlPower    Permission = "control_power"
	PermissionManageDocker    Permission = "manage_docker"
	PermissionRunScripts      Permission = "run_scripts"
	// PermissionManagePackages covers listing updates too: listing refreshes
	// the package catalogs, as root, on the machine.
	PermissionManagePackages Permission = "manage_packages"

	// Admin: manage Sentinel itself.
	PermissionManageUsers      Permission = "manage_users"
	PermissionManageEnrollment Permission = "manage_enrollment"
	PermissionManageMachines   Permission = "manage_machines"
	PermissionRevokeAgents     Permission = "revoke_agents"
	PermissionManageAlerts     Permission = "manage_alerts"
	PermissionManageSettings   Permission = "manage_settings"
	PermissionViewAuditLog     Permission = "view_audit_log"
)

// requiredRole is the table of who may do what: the least powerful role
// that has each permission. This is the only place permissions are decided.
func requiredRole(permission Permission) (Role, bool) {
	switch permission {
	case PermissionViewFleet:
		return RoleViewer, true

	case PermissionUseTerminal,
		PermissionUseDesktop,
		PermissionManageFiles,
		PermissionManageServices,
		PermissionManageProcesses,
		PermissionControlPower,
		PermissionManageDocker,
		PermissionRunScripts,
		PermissionManagePackages:
		return RoleOperator, true

	case PermissionManageUsers,
		PermissionManageEnrollment,
		PermissionManageMachines,
		PermissionRevokeAgents,
		PermissionManageAlerts,
		PermissionManageSettings,
		PermissionViewAuditLog:
		return RoleAdmin, true

	default:
		return "", false
	}
}

// Can reports whether role has permission. Unknown roles and unknown
// permissions are always refused.
func (role Role) Can(permission Permission) bool {
	required, known := requiredRole(permission)
	if !known {
		return false
	}
	return role.rank() >= required.rank() && role.rank() > 0
}

// IsValid reports whether role is one of the defined roles.
func (role Role) IsValid() bool {
	return role.rank() > 0
}

// rank orders the roles; 0 means not a real role.
func (role Role) rank() int {
	switch role {
	case RoleViewer:
		return 1
	case RoleOperator:
		return 2
	case RoleAdmin:
		return 3
	default:
		return 0
	}
}

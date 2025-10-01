package auth

import (
	"fmt"
	"net"
	"sync"
)

// Permission represents a permission level
type Permission uint8

const (
	PermissionNone Permission = iota
	PermissionRead
	PermissionWrite
	PermissionAdmin
)

// ACLRule represents an access control rule
type ACLRule struct {
	Name       string
	IPNet      *net.IPNet
	Permission Permission
	Roles      []string
	Enabled    bool
}

// ACLManager manages access control lists
type ACLManager struct {
	rules      []*ACLRule
	defaultACL Permission
	mu         sync.RWMutex
}

// NewACLManager creates a new ACL manager
func NewACLManager(defaultPermission Permission) *ACLManager {
	return &ACLManager{
		rules:      make([]*ACLRule, 0),
		defaultACL: defaultPermission,
	}
}

// AddRule adds a new ACL rule
func (m *ACLManager) AddRule(name, cidr string, permission Permission, roles []string) error {
	_, ipNet, err := net.ParseCIDR(cidr)
	if err != nil {
		return fmt.Errorf("invalid CIDR: %w", err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	rule := &ACLRule{
		Name:       name,
		IPNet:      ipNet,
		Permission: permission,
		Roles:      roles,
		Enabled:    true,
	}

	m.rules = append(m.rules, rule)
	return nil
}

// RemoveRule removes an ACL rule by name
func (m *ACLManager) RemoveRule(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for i, rule := range m.rules {
		if rule.Name == name {
			m.rules = append(m.rules[:i], m.rules[i+1:]...)
			return nil
		}
	}

	return fmt.Errorf("rule %s not found", name)
}

// CheckPermission checks if an IP has the required permission
func (m *ACLManager) CheckPermission(ip net.IP, required Permission) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Check rules in order
	for _, rule := range m.rules {
		if !rule.Enabled {
			continue
		}

		if rule.IPNet.Contains(ip) {
			return rule.Permission >= required
		}
	}

	// Use default permission if no rule matches
	return m.defaultACL >= required
}

// CheckPermissionWithRole checks permission with role consideration
func (m *ACLManager) CheckPermissionWithRole(ip net.IP, required Permission, userRoles []string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, rule := range m.rules {
		if !rule.Enabled {
			continue
		}

		if rule.IPNet.Contains(ip) {
			// Check if user has required role
			if len(rule.Roles) > 0 {
				hasRole := false
				for _, userRole := range userRoles {
					for _, ruleRole := range rule.Roles {
						if userRole == ruleRole {
							hasRole = true
							break
						}
					}
					if hasRole {
						break
					}
				}
				if !hasRole {
					continue
				}
			}

			return rule.Permission >= required
		}
	}

	return m.defaultACL >= required
}

// GetRulesForIP returns all matching rules for an IP
func (m *ACLManager) GetRulesForIP(ip net.IP) []*ACLRule {
	m.mu.RLock()
	defer m.mu.RUnlock()

	matching := make([]*ACLRule, 0)
	for _, rule := range m.rules {
		if rule.Enabled && rule.IPNet.Contains(ip) {
			matching = append(matching, rule)
		}
	}

	return matching
}

// ListRules returns all ACL rules
func (m *ACLManager) ListRules() []*ACLRule {
	m.mu.RLock()
	defer m.mu.RUnlock()

	rules := make([]*ACLRule, len(m.rules))
	copy(rules, m.rules)
	return rules
}

// EnableRule enables an ACL rule
func (m *ACLManager) EnableRule(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, rule := range m.rules {
		if rule.Name == name {
			rule.Enabled = true
			return nil
		}
	}

	return fmt.Errorf("rule %s not found", name)
}

// DisableRule disables an ACL rule
func (m *ACLManager) DisableRule(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, rule := range m.rules {
		if rule.Name == name {
			rule.Enabled = false
			return nil
		}
	}

	return fmt.Errorf("rule %s not found", name)
}

// UpdateRule updates an existing rule
func (m *ACLManager) UpdateRule(name string, permission Permission, roles []string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, rule := range m.rules {
		if rule.Name == name {
			rule.Permission = permission
			rule.Roles = roles
			return nil
		}
	}

	return fmt.Errorf("rule %s not found", name)
}

// SetDefaultPermission sets the default permission level
func (m *ACLManager) SetDefaultPermission(permission Permission) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.defaultACL = permission
}

// GetDefaultPermission returns the default permission level
func (m *ACLManager) GetDefaultPermission() Permission {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.defaultACL
}

// String returns a string representation of a permission
func (p Permission) String() string {
	switch p {
	case PermissionNone:
		return "none"
	case PermissionRead:
		return "read"
	case PermissionWrite:
		return "write"
	case PermissionAdmin:
		return "admin"
	default:
		return "unknown"
	}
}

// ParsePermission converts a string to Permission
func ParsePermission(s string) (Permission, error) {
	switch s {
	case "none":
		return PermissionNone, nil
	case "read":
		return PermissionRead, nil
	case "write":
		return PermissionWrite, nil
	case "admin":
		return PermissionAdmin, nil
	default:
		return PermissionNone, fmt.Errorf("unknown permission: %s", s)
	}
}

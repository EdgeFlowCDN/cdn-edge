package proxy

import "testing"

func TestIPACLBlacklist(t *testing.T) {
	acl := NewIPACL(IPACLBlacklist)
	acl.AddIP("1.2.3.4")
	acl.AddIP("10.0.0.0/8")

	if acl.IsAllowed("1.2.3.4") {
		t.Error("blacklisted IP should be blocked")
	}
	if acl.IsAllowed("10.0.1.1") {
		t.Error("CIDR range IP should be blocked")
	}
	if !acl.IsAllowed("8.8.8.8") {
		t.Error("non-blacklisted IP should be allowed")
	}
}

func TestIPACLWhitelist(t *testing.T) {
	acl := NewIPACL(IPACLWhitelist)
	acl.AddIP("1.2.3.4")
	acl.AddIP("192.168.0.0/16")

	if !acl.IsAllowed("1.2.3.4") {
		t.Error("whitelisted IP should be allowed")
	}
	if !acl.IsAllowed("192.168.1.100") {
		t.Error("whitelisted CIDR IP should be allowed")
	}
	if acl.IsAllowed("8.8.8.8") {
		t.Error("non-whitelisted IP should be blocked")
	}
}

func TestIPACLRemove(t *testing.T) {
	acl := NewIPACL(IPACLBlacklist)
	acl.AddIP("1.2.3.4")
	acl.RemoveIP("1.2.3.4")
	if !acl.IsAllowed("1.2.3.4") {
		t.Error("removed IP should be allowed")
	}
}

func TestPerDomainIPACL(t *testing.T) {
	pd := NewPerDomainIPACL()

	blacklist := NewIPACL(IPACLBlacklist)
	blacklist.AddIP("1.2.3.4")
	pd.SetACL("example.com", blacklist)

	if pd.IsAllowed("example.com", "1.2.3.4") {
		t.Error("should be blocked for example.com")
	}
	if !pd.IsAllowed("other.com", "1.2.3.4") {
		t.Error("should be allowed for other.com (no ACL)")
	}
}

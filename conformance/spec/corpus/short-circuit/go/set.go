package fixture

// option is a setting that is on or off.
type option struct {
	on bool
}

// set reports whether p is an option that is on.
func set(p *option) bool {
	return p != nil && p.on
}

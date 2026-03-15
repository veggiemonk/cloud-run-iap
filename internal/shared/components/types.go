package components

// NavItem represents a single navigation link.
type NavItem struct {
	Href  string
	Label string
	Page  string
}

// NavConfig holds navigation configuration for the layout.
type NavConfig struct {
	Brand string
	Items []NavItem
}

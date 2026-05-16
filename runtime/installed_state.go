package runtime

// InstalledStateRecord is a single installed-state entry from a node agent.
type InstalledStateRecord struct {
	ServiceID string
	Version   string
	BuildID   string
	NodeID    string
	Status    string // INSTALLED, RUNNING, FAILED, UNKNOWN
	Checksum  string
}

package workspace

import "github.com/sshctl/sshctl/internal/catalog"

// Snapshot is what the Workspace loads at startup and on refresh. Its
// Collapsed groups apply at startup only.
type Snapshot = catalog.Snapshot

// Catalog is the Connection catalog as the Workspace uses it: everything the
// Workspace reads and saves goes through it. The real one is
// *catalog.Catalog.
type Catalog interface {
	// Snapshot reads the Connection list fresh from disk.
	Snapshot() (Snapshot, error)
	// RecordOpen records in History that alias was opened.
	RecordOpen(alias string) error
	// History returns the aliases opened, most recent first.
	History() ([]string, error)
	// SetGroup saves alias's group ("" for none).
	SetGroup(alias, group string) error
	// SetCollapsed saves every group the list shows collapsed.
	SetCollapsed(groups []string) error
	// Save adds a Connection (old is "") or saves changes to old. It returns
	// an *IncludeError when ssh_config must first read sshctl's hosts file.
	Save(old string, s Spec) error
	// Delete deletes alias's Host block when sshctl manages it, and its
	// sshctl metadata.
	Delete(alias string) error
	// AddInclude makes ssh_config read sshctl's hosts file.
	AddInclude() error
	// The Group methods take the Space by name, "" for the default Space.

	// MoveGroup puts space's Group name at position among space's Groups,
	// counting from 1.
	MoveGroup(space, name string, position int) error
	// AddGroup creates an empty Group after space's others.
	AddGroup(space, name string) error
	// RenameGroup renames a Group; its members move with it.
	RenameGroup(space, old, name string) error
	// DeleteGroup deletes a Group; its members become ungrouped.
	DeleteGroup(space, name string) error
	// SetActiveSpace saves the Space the Workspace shows, by name ("" for
	// the default Space).
	SetActiveSpace(space string) error
	// AddSpace creates an empty Space with the lowest free Space number.
	AddSpace(name string) error
	// AddSpaceAt creates an empty Space with the free Space number n.
	AddSpaceAt(name string, n int) error
	// RenameSpace renames a Space; its Groups and Connections, collapsed
	// Groups and whether it is active move with it.
	RenameSpace(old, name string) error
	// DeleteSpace deletes an empty Space other than the default Space.
	DeleteSpace(name string) error
	// MoveSpace gives a Space the Space number n, swapping numbers with
	// the Space that has it.
	MoveSpace(name string, n int) error
	// Keys lists the key files a Selected identity is chosen from, as
	// "sshctl key list" shows them.
	Keys() ([]string, error)
	// SetIdentity makes key (a private key path) the global Selected
	// identity; "" leaves the choice of keys to OpenSSH.
	SetIdentity(key string) error
	// SetSpaceIdentity makes key the Space space's Selected identity; ""
	// unsets it, so the global one applies.
	SetSpaceIdentity(space, key string) error
	// ProvisionRepo returns the dotfiles repo Provision applies to alias,
	// catalog.ErrNoDotfiles when none is set, or an error wrapping
	// catalog.ErrProvisionRefused when the setting that applies is none.
	ProvisionRepo(alias string) (string, error)
}

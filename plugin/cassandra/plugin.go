// The `cassandra` plugin for SHIELD implements backup + restore of one single
// keyspace on a Cassandra node.
//
// PLUGIN FEATURES
//
// This plugin implements functionality suitable for use with the following
// SHIELD Job components:
//
//   Target: yes
//   Store:  no
//
// PLUGIN CONFIGURATION
//
// The endpoint configuration passed to this plugin is used to identify which
// cassandra node to back up, and how to connect to it. Your endpoint JSON
// should look something like this:
//
//    {
//        "cassandra_host"         : "10.244.67.61",
//        "cassandra_port"         : "9042",             # native transport port
//        "cassandra_user"         : "username",
//        "cassandra_password"     : "password",
//        "cassandra_include_keyspaces"     : "ksXXXX",           # optional
//        "cassandra_exclude_keyspaces"     : "ksXXXX",           # optional
//        "cassandra_bindir"       : "/path/to/bindir",
//        "cassandra_datadir"      : "/path/to/datadir",
//        "cassandra_tar"          : "/path/to/tar"      # where is the tar utility?
//    }
//
// The plugin provides devault values for those configuration properties, as
// detailed below. When a default value suits your needs, you can just ommit
// it.
//
//    {
//        "cassandra_host"     : "127.0.0.1",
//        "cassandra_port"     : "9042",
//        "cassandra_user"     : "cassandra",
//        "cassandra_password" : "cassandra",
//        "cassandra_include_keyspaces" : "", # Backup all keyspaces
//        "cassandra_exclude_keyspaces" : "system_schema system_distributed system_auth system system_traces",
//        "cassandra_bindir"   : "/var/vcap/packages/cassandra/bin",
//        "cassandra_datadir"  : "/var/vcap/store/cassandra/data",
//        "cassandra_tar"      : "tar"
//    }
//
// BACKUP DETAILS
//
// When no keyspace is specified, then all keyspaces are backed up on a
// specific node. To completely backup the Cassandra cluster, the backup
// operation needs to be performed on all cluster nodes.
//
// Otherwise, backup is limited to one single keyspace, and is made against
// one single node. To completely backup the given keyspace, the backup
// operation needs to be performed on all cluster nodes.
//
// RESTORE DETAILS
//
// When no keyspace is specified, then all keyspaces are restored on a
// specific node. To completely restore the Cassandra cluster, the restore
// operation needs to be performed on all cluster nodes.
//
// Restore is limited to the single keyspace specified in the plugin config.
// When restoring, this keyspace config must be the same as the keyspace
// specified at backup time. Indeed, this plugin doesn't support restoring to
// a different keyspace.
//
// Restore should happen on the same node where the data has been backed up.
// To completely restore a keyspace, the restore operation should be performed
// on each node of the cluster, with the data that was backed up on that same
// node.
//
// DEPENDENCIES
//
// This plugin relies on the `nodetool` and `sstableloader` utilities. Please
// ensure that they are present on the cassandra node that will be backed up
// or restored.

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/starkandwayne/goutils/ansi"

	"github.com/starkandwayne/shield/plugin"
)

// Default configuration values for the plugin
const (
	DefaultHost             = "127.0.0.1"
	DefaultPort             = "9042"
	DefaultUser             = "cassandra"
	DefaultPassword         = "cassandra"
	DefaultExcludeKeyspaces = []string{"system_schema", "system_distributed", "system_auth", "system", "system_traces"}
	DefaultBinDir           = "/var/vcap/jobs/cassandra/bin"
	DefaultDataDir          = "/var/vcap/store/cassandra/data"
	DefaultTar              = "tar"

	VcapOwnership = "vcap:vcap"
	SnapshotName  = "shield-backup"
)

func main() {
	p := CassandraPlugin{
		Name:    "Cassandra Backup Plugin",
		Author:  "Orange",
		Version: "0.2.0",
		Features: plugin.PluginFeatures{
			Target: "yes",
			Store:  "no",
		},
		Example: `
{
  "cassandra_host"              : "127.0.0.1",      # optional
  "cassandra_port"              : "9042",           # optional
  "cassandra_user"              : "username",
  "cassandra_password"          : "password",
  "cassandra_include_keyspaces" : "db",
  "cassandra_exclude_keyspaces" : "system",
  "cassandra_bindir"            : "/path/to/bin",   # optional
  "cassandra_datadir"           : "/path/to/data",  # optional
  "cassandra_tar"               : "/bin/tar"        # Tar-compatible archival tool to use
}
`,
		Defaults: `
{
  "cassandra_host"              : "127.0.0.1",
  "cassandra_port"              : "9042",
  "cassandra_user"              : "cassandra",
  "cassandra_password"          : "cassandra",
  "cassandra_exclude_keyspaces" : "system_schema system_distributed system_auth system system_traces",
  "cassandra_bindir"            : "/var/vcap/packages/cassandra/bin",
  "cassandra_datadir"           : "/var/vcap/store/cassandra/data",
  "cassandra_tar"               : "tar"
}
`,
	}

	plugin.Run(p)
}

// CassandraPlugin declares the custom type for plugin config
type CassandraPlugin plugin.PluginInfo

// CassandraInfo defines the custom type for plugin config
type CassandraInfo struct {
	Host             string
	Port             string
	User             string
	Password         string
	IncludeKeyspaces []string
	ExcludeKeyspaces []string
	BinDir           string
	DataDir          string
	Tar              string
}

// Meta returns the plugin's PluginInfo, however you decide to implement it
func (p CassandraPlugin) Meta() plugin.PluginInfo {
	return plugin.PluginInfo(p)
}

// Validate validates endpoints from the command line
func (p CassandraPlugin) Validate(endpoint plugin.ShieldEndpoint) error {
	var (
		a    []string
		s    string
		err  error
		fail bool
	)

	s, err = endpoint.StringValueDefault("cassandra_host", "")
	if err != nil {
		ansi.Printf("@R{\u2717 cassandra_host          %s}\n", err)
		fail = true
	} else if s == "" {
		ansi.Printf("@G{\u2713 cassandra_host}          using default node @C{%s}\n", DefaultHost)
	} else {
		ansi.Printf("@G{\u2713 cassandra_host}          @C{%s}\n", s)
	}

	s, err = endpoint.StringValueDefault("cassandra_port", "")
	if err != nil {
		ansi.Printf("@R{\u2717 cassandra_port          %s}\n", err)
		fail = true
	} else if s == "" {
		ansi.Printf("@G{\u2713 cassandra_port}          using default port @C{%s}\n", DefaultPort)
	} else {
		ansi.Printf("@G{\u2713 cassandra_port}          @C{%s}\n", s)
	}

	s, err = endpoint.StringValueDefault("cassandra_user", "")
	if err != nil {
		ansi.Printf("@R{\u2717 cassandra_user          %s}\n", err)
		fail = true
	} else if s == "" {
		ansi.Printf("@G{\u2713 cassandra_user}          using default user @C{%s}\n", DefaultUser)
	} else {
		ansi.Printf("@G{\u2713 cassandra_user}          @C{%s}\n", s)
	}

	s, err = endpoint.StringValueDefault("cassandra_password", "")
	if err != nil {
		ansi.Printf("@R{\u2717 cassandra_password      %s}\n", err)
		fail = true
	} else if s == "" {
		ansi.Printf("@G{\u2713 cassandra_password}      using default password @C{%s}\n", DefaultPassword)
	} else {
		ansi.Printf("@G{\u2713 cassandra_password}      @C{%s}\n", s)
	}

	a, err = endpoint.ArrayValueDefault("cassandra_include_keyspaces", nil)
	if err != nil {
		ansi.Printf("@R{\u2717 cassandra_include_keyspaces      %s}\n", err)
		fail = true
	} else if a == nil {
		ansi.Printf("@G{\u2713 cassandra_include_keyspaces}      backing up *all* keyspaces\n")
	} else {
		ansi.Printf("@G{\u2713 cassandra_include_keyspaces}      [@C{%v}]\n", a)
	}

	a, err = endpoint.ArrayValueDefault("cassandra_exclude_keyspace", DefaultExcludeKeyspaces)
	if err != nil {
		ansi.Printf("@R{\u2717 cassandra_exclude_keyspaces      %s}\n", err)
		fail = true
	} else if len(a) == 0 {
		ansi.Printf("@G{\u2713 cassandra_exclude_keyspaces}      including *all* keyspaces\n")
	} else {
		ansi.Printf("@G{\u2713 cassandra_exclude_keyspaces}      [@C{%v}]\n", a)
	}

	s, err = endpoint.StringValueDefault("cassandra_bindir", "")
	if err != nil {
		ansi.Printf("@R{\u2717 cassandra_bindir        %s}\n", err)
		fail = true
	} else if s == "" {
		ansi.Printf("@G{\u2713 cassandra_bindir}        using default @C{%s}\n", DefaultBinDir)
	} else {
		ansi.Printf("@G{\u2713 cassandra_bindir}        @C{%s}\n", s)
	}

	s, err = endpoint.StringValueDefault("cassandra_datadir", "")
	if err != nil {
		ansi.Printf("@R{\u2717 cassandra_datadir       %s}\n", err)
		fail = true
	} else if s == "" {
		ansi.Printf("@G{\u2713 cassandra_datadir}       using default @C{%s}\n", DefaultDataDir)
	} else {
		ansi.Printf("@G{\u2713 cassandra_datadir}       @C{%s}\n", s)
	}

	s, err = endpoint.StringValueDefault("cassandra_tar", "")
	if err != nil {
		ansi.Printf("@R{\u2717 cassandra_tar           %s}\n", err)
		fail = true
	} else if s == "" {
		ansi.Printf("@G{\u2713 cassandra_tar}           using default @C{%s}\n", DefaultTar)
	} else {
		ansi.Printf("@G{\u2713 cassandra_tar}           @C{%s}\n", s)
	}

	if fail {
		return fmt.Errorf("cassandra: invalid configuration")
	}
	return nil
}

// Backup one cassandra keyspace
func (p CassandraPlugin) Backup(endpoint plugin.ShieldEndpoint) error {
	cassandra, err := cassandraInfo(endpoint)
	if err != nil {
		return err
	}

	plugin.DEBUG("Cleaning any stale '%s' snapshot", SnapshotName)
	cmd := fmt.Sprintf("%s/nodetool clearsnapshot -t %s", cassandra.BinDir, SnapshotName)
	plugin.DEBUG("Executing: `%s`", cmd)
	err = plugin.Exec(cmd, plugin.STDIN)
	if err != nil {
		ansi.Fprintf(os.Stderr, "@R{\u2717 Clean up any stale snapshot}\n")
		return err
	}
	ansi.Fprintf(os.Stderr, "@G{\u2713 Clean up any stale snapshot}\n")

	defer func() {
		plugin.DEBUG("Clearing snapshot '%s'", SnapshotName)
		cmd := fmt.Sprintf("%s/nodetool clearsnapshot -t %s", cassandra.BinDir, SnapshotName)
		plugin.DEBUG("Executing: `%s`", cmd)
		err := plugin.Exec(cmd, plugin.STDIN)
		if err != nil {
			ansi.Fprintf(os.Stderr, "@R{\u2717 Clear snapshot}\n")
			return
		}
		ansi.Fprintf(os.Stderr, "@G{\u2713 Clear snapshot}\n")
	}()

	var savedKeyspaces []string
	if cassandra.IncludeKeyspaces != nil {
		sort.Strings(cassandra.ExcludeKeyspaces)
		savedKeyspaces = []string{}
		for _, keyspace := range cassandra.IncludeKeyspaces {
			idx := sort.SearchStrings(cassandra.ExcludeKeyspaces, keyspace)
			if idx < len(cassandra.ExcludeKeyspaces) && cassandra.ExcludeKeyspaces[idx] == keyspace {
				continue
			}
			append(savedKeyspaces, keyspace)
		}
	}
	sort.Strings(savedKeyspaces)

	plugin.DEBUG("Creating a new '%s' snapshot", SnapshotName)
	cmd = fmt.Sprintf("%s/nodetool snapshot -t %s", cassandra.BinDir, SnapshotName)
	if savedKeyspaces != nil {
		for _, keyspace := range savedKeyspaces {
			cmd = fmt.Sprintf("%s \"%s\"", cmd, keyspace)
		}
	}
	plugin.DEBUG("Executing: `%s`", cmd)
	err = plugin.Exec(cmd, plugin.STDIN)
	if err != nil {
		ansi.Fprintf(os.Stderr, "@R{\u2717 Create new snapshot}\n")
		return err
	}
	ansi.Fprintf(os.Stderr, "@G{\u2713 Create new snapshot}\n")

	// Here we need to copy the snapshots/shield-backup directories into a
	// {keyspace}/{tablename} structure that we'll temporarily put in
	// /var/vcap/store/shield/cassandra. Then we can tar it all and stream
	// that to stdout.

	baseDir := "/var/vcap/store/shield/cassandra"

	// Recursively remove /var/vcap/store/shield/cassandra, if any
	plugin.DEBUG("Removing any stale '%s' directory", baseDir)
	cmd = fmt.Sprintf("rm -rf \"%s\"", baseDir)
	plugin.DEBUG("Executing `%s`", cmd)
	err = plugin.Exec(cmd, plugin.STDOUT)
	if err != nil {
		ansi.Fprintf(os.Stderr, "@R{\u2717 Clean up any stale base temporary directory}\n")
		return err
	}
	ansi.Fprintf(os.Stderr, "@G{\u2713 Clean up any stale base temporary directory}\n")

	plugin.DEBUG("Creating base directories for '%s', with 0755 permissions", baseDir)
	err = os.MkdirAll(baseDir, 0755)
	if err != nil {
		ansi.Fprintf(os.Stderr, "@R{\u2717 Create base temporary directory}\n")
		return err
	}
	ansi.Fprintf(os.Stderr, "@G{\u2713 Create base temporary directory}\n")

	defer func() {
		// Recursively remove /var/vcap/store/shield/cassandra directory
		plugin.DEBUG("Cleaning the '%s' directory up", baseDir)
		cmd := fmt.Sprintf("rm -rf \"%s\"", baseDir)
		plugin.DEBUG("Executing `%s`", cmd)
		err := plugin.Exec(cmd, plugin.STDOUT)
		if err != nil {
			ansi.Fprintf(os.Stderr, "@R{\u2717 Clear base temporary directory}\n")
			return
		}
		ansi.Fprintf(os.Stderr, "@G{\u2713 Clear base temporary directory}\n")
	}()

	// Iterate through {dataDir}/{keyspace}/{tablename}/snapshots/shield-backup/*
	// and for all the immutable files we find here, we hard-link them
	// to /var/vcap/store/shield/cassandra/{keyspace}/{tablename}
	//
	// We chose to hard-link because copying those immutable files is
	// unnecessary anyway. It could lead to performance issues and would
	// consume twice the disk space it should.

	info, err := os.Lstat(cassandra.DataDir)
	if err != nil {
		ansi.Fprintf(os.Stderr, "@R{\u2717 Recursive hard-link snapshot files in temp dir}\n")
		return err
	}
	if !info.IsDir() {
		ansi.Fprintf(os.Stderr, "@R{\u2717 Recursive hard-link snapshot files in temp dir}\n")
		return fmt.Errorf("cassandra DataDir is not a directory")
	}

	dir, err := os.Open(cassandra.DataDir)
	if err != nil {
		ansi.Fprintf(os.Stderr, "@R{\u2717 Recursive hard-link snapshot files in temp dir}\n")
		return err
	}
	defer dir.Close()

	entries, err := dir.Readdir(-1)
	if err != nil {
		ansi.Fprintf(os.Stderr, "@R{\u2717 Recursive hard-link snapshot files in temp dir}\n")
		return err
	}
	for _, keyspaceDirInfo := range entries {
		if !keyspaceDirInfo.IsDir() {
			continue
		}
		keyspace := keyspaceDirInfo.Name()
		if savedKeyspaces == nil {
			idx := sort.SearchStrings(cassandra.ExcludeKeyspaces, keyspace)
			if idx < len(cassandra.ExcludeKeyspaces) && cassandra.ExcludeKeyspaces[idx] == keyspace {
				plugin.DEBUG("Excluding keyspace '%s'", keyspace)
				continue
			}
		} else {
			idx := sort.SearchStrings(savedKeyspaces, keyspace)
			if idx >= len(savedKeyspaces) || savedKeyspaces[idx] != keyspace {
				plugin.DEBUG("Excluding keyspace '%s'", keyspace)
				continue
			}
		}
		err = hardLinkKeyspace(cassandra.DataDir, baseDir, keyspace)
		if err != nil {
			ansi.Fprintf(os.Stderr, "@R{\u2717 Recursive hard-link snapshot files in temp dir}\n")
			return err
		}
	}
	ansi.Fprintf(os.Stderr, "@G{\u2713 Recursive hard-link snapshot files in temp dir}\n")

	plugin.DEBUG("Setting ownership of all backup files to '%s'", VcapOwnership)
	cmd = fmt.Sprintf("chown -R vcap:vcap \"%s\"", baseDir)
	plugin.DEBUG("Executing `%s`", cmd)
	err = plugin.Exec(cmd, plugin.STDOUT)
	if err != nil {
		ansi.Fprintf(os.Stderr, "@R{\u2717 Set ownership of snapshot hard-links}\n")
		return err
	}
	ansi.Fprintf(os.Stderr, "@G{\u2713 Set ownership of snapshot hard-links}\n")

	plugin.DEBUG("Streaming output tar file")
	cmd = fmt.Sprintf("%s -c -C %s -f - .", cassandra.Tar, baseDir)
	plugin.DEBUG("Executing `%s`", cmd)
	err = plugin.Exec(cmd, plugin.STDOUT)
	if err != nil {
		ansi.Fprintf(os.Stderr, "@R{\u2717 Stream tar of snapshots files}\n")
		return err
	}
	ansi.Fprintf(os.Stderr, "@G{\u2713 Stream tar of snapshots files}\n")

	return nil
}

func hardLinkKeyspace(srcDataDir string, dstBaseDir string, keyspace string) error {
	tmpKeyspaceDir := filepath.Join(dstBaseDir, keyspace)
	plugin.DEBUG("Creating destination keyspace directory '%s' with 0700 permissions", tmpKeyspaceDir)
	err := os.Mkdir(tmpKeyspaceDir, 0700)
	if err != nil {
		return err
	}

	srcKeyspaceDir := filepath.Join(srcDataDir, keyspace)
	dir, err := os.Open(srcKeyspaceDir)
	if err != nil {
		return err
	}
	defer dir.Close()

	entries, err := dir.Readdir(-1)
	if err != nil {
		return err
	}
	for _, tableDirInfo := range entries {
		if !tableDirInfo.IsDir() {
			continue
		}

		srcDir := filepath.Join(srcKeyspaceDir, tableDirInfo.Name(), "snapshots", SnapshotName)
		_, err = os.Lstat(srcDir)
		if os.IsNotExist(err) {
			continue
		} else if err != nil {
			return err
		}

		tableName := tableDirInfo.Name()
		if idx := strings.LastIndex(tableName, "-"); idx >= 0 {
			tableName = tableName[:idx]
		}

		dstDir := filepath.Join(tmpKeyspaceDir, tableName)
		plugin.DEBUG("Creating destination table directory '%s'", dstDir)
		err = os.MkdirAll(dstDir, 0755)
		if err != nil {
			return err
		}

		plugin.DEBUG("Hard-linking all '%s/*' files to '%s/'", srcDir, dstDir)
		err = hardLinkAll(srcDir, dstDir)
		if err != nil {
			return err
		}
	}
	return nil
}

// Hard-link all files from 'srcDir' to the 'dstDir'
func hardLinkAll(srcDir string, dstDir string) (err error) {

	dir, err := os.Open(srcDir)
	if err != nil {
		return err
	}
	defer func() {
		dir.Close()
	}()

	entries, err := dir.Readdir(-1)
	if err != nil {
		return err
	}

	for _, tableDirInfo := range entries {
		if tableDirInfo.IsDir() {
			continue
		}
		src := filepath.Join(srcDir, tableDirInfo.Name())
		dst := filepath.Join(dstDir, tableDirInfo.Name())

		err = os.Link(src, dst)
		if err != nil {
			return err
		}
	}
	return nil
}

// Restore one cassandra keyspace
func (p CassandraPlugin) Restore(endpoint plugin.ShieldEndpoint) error {
	cassandra, err := cassandraInfo(endpoint)
	if err != nil {
		return err
	}

	baseDir := "/var/vcap/store/shield/cassandra"

	// Recursively remove /var/vcap/store/shield/cassandra, if any
	cmd := fmt.Sprintf("rm -rf \"%s\"", baseDir)
	plugin.DEBUG("Executing `%s`", cmd)
	err = plugin.Exec(cmd, plugin.STDOUT)
	if err != nil {
		ansi.Fprintf(os.Stderr, "@R{\u2717 Clean up any stale base temporary directory}\n")
		return err
	}
	ansi.Fprintf(os.Stderr, "@G{\u2713 Clean up any stale base temporary directory}\n")

	plugin.DEBUG("Creating directory '%s' with 0755 permissions", baseDir)
	err = os.MkdirAll(baseDir, 0755)
	if err != nil {
		ansi.Fprintf(os.Stderr, "@R{\u2717 Create base temporary directory}\n")
		return err
	}
	ansi.Fprintf(os.Stderr, "@G{\u2713 Create base temporary directory}\n")

	defer func() {
		// Recursively remove /var/vcap/store/shield/cassandra, if any
		cmd := fmt.Sprintf("rm -rf \"%s\"", baseDir)
		plugin.DEBUG("Executing `%s`", cmd)
		err := plugin.Exec(cmd, plugin.STDOUT)
		if err != nil {
			ansi.Fprintf(os.Stderr, "@R{\u2717 Clear base temporary directory}\n")
			return
		}
		ansi.Fprintf(os.Stderr, "@G{\u2713 Clear base temporary directory}\n")
	}()

	cmd = fmt.Sprintf("%s -x -C %s -f -", cassandra.Tar, baseDir)
	plugin.DEBUG("Executing `%s`", cmd)
	err = plugin.Exec(cmd, plugin.STDIN)
	if err != nil {
		ansi.Fprintf(os.Stderr, "@R{\u2717 Extract tar to temporary directory}\n")
		return err
	}
	ansi.Fprintf(os.Stderr, "@G{\u2713 Extract tar to temporary directory}\n")

	dir, err := os.Open(baseDir)
	if err != nil {
		ansi.Fprintf(os.Stderr, "@R{\u2717 Load tables data}\n")
		return err
	}
	defer dir.Close()

	entries, err := dir.Readdir(-1)
	if err != nil {
		ansi.Fprintf(os.Stderr, "@R{\u2717 Load tables data}\n")
		return err
	}
	for _, keyspaceDirInfo := range entries {
		if !keyspaceDirInfo.IsDir() {
			continue
		}
		keyspace := keyspaceDirInfo.Name()
		if savedKeyspaces == nil {
			idx := sort.SearchStrings(cassandra.ExcludeKeyspaces, keyspace)
			if idx < len(cassandra.ExcludeKeyspaces) && cassandra.ExcludeKeyspaces[idx] == keyspace {
				plugin.DEBUG("Excluding keyspace '%s'", keyspace)
				continue
			}
		} else {
			idx := sort.SearchStrings(savedKeyspaces, keyspace)
			if idx >= len(savedKeyspaces) || savedKeyspaces[idx] != keyspace {
				plugin.DEBUG("Excluding keyspace '%s'", keyspace)
				continue
			}
		}
		keyspaceDirPath := filepath.Join(baseDir, keyspace)
		err = restoreKeyspace(cassandra, keyspaceDirPath)
		if err != nil {
			ansi.Fprintf(os.Stderr, "@R{\u2717 Load tables data for keyspace '%s'}\n", keyspace)
			return err
		}
		ansi.Fprintf(os.Stderr, "@G{\u2713 Load tables data for keyspace '%s'}\n", keyspace)
	}
	ansi.Fprintf(os.Stderr, "@G{\u2713 Load tables data}\n")

	return nil
}

func restoreKeyspace(cassandra *CassandraInfo, keyspaceDirPath string) error {
	// Iterate through all table directories /var/vcap/store/shield/cassandra/{cassandra.IncludeKeyspaces}/{tablename}
	dir, err := os.Open(keyspaceDirPath)
	if err != nil {
		return err
	}
	defer dir.Close()

	entries, err := dir.Readdir(-1)
	if err != nil {
		return err
	}
	for _, tableDirInfo := range entries {
		if !tableDirInfo.IsDir() {
			continue
		}
		// Run sstableloader on each sub-directory found, assuming it is a table backup
		tableDirPath := filepath.Join(keyspaceDirPath, tableDirInfo.Name())
		cmd := fmt.Sprintf("%s/sstableloader -u \"%s\" -pw \"%s\" -d \"%s\" \"%s\"", cassandra.BinDir, cassandra.User, cassandra.Password, cassandra.Host, tableDirPath)
		plugin.DEBUG("Executing: `%s`", cmd)
		err = plugin.Exec(cmd, plugin.STDIN)
		if err != nil {
			return err
		}
	}
	return nil
}

// Store is unimplemented
func (p CassandraPlugin) Store(endpoint plugin.ShieldEndpoint) (string, error) {
	return "", plugin.UNIMPLEMENTED
}

// Retrieve is unimplemented
func (p CassandraPlugin) Retrieve(endpoint plugin.ShieldEndpoint, file string) error {
	return plugin.UNIMPLEMENTED
}

// Purge is unimplemented
func (p CassandraPlugin) Purge(endpoint plugin.ShieldEndpoint, key string) error {
	return plugin.UNIMPLEMENTED
}

func cassandraInfo(endpoint plugin.ShieldEndpoint) (*CassandraInfo, error) {
	host, err := endpoint.StringValueDefault("cassandra_host", DefaultHost)
	if err != nil {
		return nil, err
	}
	plugin.DEBUG("CASSANDRA_HOST: '%s'", host)

	port, err := endpoint.StringValueDefault("cassandra_port", DefaultPort)
	if err != nil {
		return nil, err
	}
	plugin.DEBUG("CASSANDRA_PORT: '%s'", port)

	user, err := endpoint.StringValueDefault("cassandra_user", DefaultUser)
	if err != nil {
		return nil, err
	}
	plugin.DEBUG("CASSANDRA_USER: '%s'", user)

	password, err := endpoint.StringValueDefault("cassandra_password", DefaultPassword)
	if err != nil {
		return nil, err
	}
	plugin.DEBUG("CASSANDRA_PWD: '%s'", password)

	includeKeyspace, err := endpoint.ArrayValueDefault("cassandra_include_keyspaces", nil)
	if err != nil {
		return nil, err
	}
	plugin.DEBUG("CASSANDRA_INCLUDE_KEYSPACES: [%v]", includeKeyspace)

	excludeKeyspace, err := endpoint.ArrayValueDefault("cassandra_exclude_keyspaces", DefaultExcludeKeyspaces)
	if err != nil {
		return nil, err
	}
	plugin.DEBUG("CASSANDRA_EXCLUDE_KEYSPACES: [%v]", excludeKeyspace)

	bindir, err := endpoint.StringValueDefault("cassandra_bindir", DefaultBinDir)
	if err != nil {
		return nil, err
	}
	plugin.DEBUG("CASSANDRA_BINDIR: '%s'", bindir)

	datadir, err := endpoint.StringValueDefault("cassandra_datadir", DefaultDataDir)
	if err != nil {
		return nil, err
	}
	plugin.DEBUG("CASSANDRA_DATADIR: '%s'", datadir)

	tar, err := endpoint.StringValueDefault("cassandra_tar", DefaultTar)
	if err != nil {
		return nil, err
	}
	plugin.DEBUG("CASSANDRA_TAR: '%s'", tar)

	return &CassandraInfo{
		Host:             host,
		Port:             port,
		User:             user,
		Password:         password,
		IncludeKeyspaces: includeKeyspace,
		ExcludeKeyspaces: excludeKeyspace,
		BinDir:           bindir,
		DataDir:          datadir,
		Tar:              tar,
	}, nil
}

package snapshot

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/inCoderKaka/local-server/internal/settings"
	"github.com/inCoderKaka/local-server/internal/storage"
)

// Keys of the backup settings. All are owned by the host and read at the moment of use (req_001
// D-38, D-41, D-54); a change applies to the next run, except backup.enabled and
// backup.interval, which apply when the worker next wakes.
const (
	KeyEnabled         = "backup.enabled"
	KeyInterval        = "backup.interval"
	KeyTarget          = "backup.target"
	KeyFolderPath      = "backup.folder.path"
	KeyS3Endpoint      = "backup.s3.endpoint"
	KeyS3Region        = "backup.s3.region"
	KeyS3Bucket        = "backup.s3.bucket"
	KeyS3Prefix        = "backup.s3.prefix"
	KeyS3PathStyle     = "backup.s3.path_style"
	KeyS3PartSize      = "backup.s3.part_size_bytes"
	KeyS3SealedSecret  = "backup.s3.sealed_secret"
	KeyRetentionCount  = "backup.retention_count"
	KeyRetentionAge    = "backup.retention_age"
	KeyChunkBytes      = "backup.chunk_bytes"
	KeyTempDir         = "backup.temp_dir"
	KeyRunTimeout      = "backup.run_timeout"
	KeyNotifyInterval  = "backup.failure_notify_interval"
	KeyLastSuccessAt   = "backup.last_success_at"
	KeyMinimizePrefix  = "backup.minimize."
	defaultInterval    = 5 * time.Minute
	defaultNotifyEvery = time.Hour
)

// defaultMinimize are the tables that default to minimize (req_001 D-64): the job content
// tables and the login sessions. Every other table defaults to keep as is. The set satisfies
// D-69 over the migrated schema; a test asserts it.
var defaultMinimize = map[string]bool{"sch_job_payload": true, "sch_job_result": true, "idn_session": true}

// MinimizeKey is the setting key of a table's minimize switch.
func MinimizeKey(table string) string { return KeyMinimizePrefix + table }

// DefaultMinimize reports whether a table defaults to minimize. A protected table never does
// (req_001 D-139).
func DefaultMinimize(table string) bool { return defaultMinimize[table] && !IsProtected(table) }

func boolText(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

// Defs returns the backup settings that do not depend on the schema; RegisterSettings adds one
// minimize switch per catalog table.
func Defs() []settings.Def {
	h := settings.Host
	return []settings.Def{
		{Key: KeyEnabled, Kind: settings.Bool, Default: "false", Owner: h,
			Description: "Whether local-server backs up its database. Off until you choose a destination: set backup.target and its folder or bucket first, and for a bucket store its access key (local-server backup-credentials)."},
		{Key: KeyInterval, Kind: settings.Duration, Default: defaultInterval.String(), Owner: h,
			Description: "How often a backup is taken. A backup is a compressed, encrypted copy of the database sent to the destination; it runs in the background and never delays a request. A change applies when the backup worker next wakes."},
		{Key: KeyTarget, Kind: settings.String, Default: TargetS3, Owner: h,
			Description: "Where backups go: s3 (a bucket you own that speaks the S3 protocol: Cloudflare R2, Google Cloud Storage or another) or folder (a folder on this machine or a drive you mount)."},
		{Key: KeyFolderPath, Kind: settings.String, Default: "", Owner: h,
			Description: "The folder backups are written to when backup.target is folder. It is created if it does not exist. Backups in a folder on the same disk do not survive that disk failing."},
		{Key: KeyS3Endpoint, Kind: settings.String, Default: "", Owner: h,
			Description: "The service address of the bucket, for example https://<account id>.r2.cloudflarestorage.com for Cloudflare R2 or https://storage.googleapis.com for Google Cloud Storage (with an HMAC key). Empty means Amazon S3."},
		{Key: KeyS3Region, Kind: settings.String, Default: "auto", Owner: h,
			Description: "The bucket's region. Cloudflare R2 and Google Cloud Storage accept auto."},
		{Key: KeyS3Bucket, Kind: settings.String, Default: "", Owner: h,
			Description: "The name of the bucket backups go to."},
		{Key: KeyS3Prefix, Kind: settings.String, Default: "local-server/", Owner: h,
			Description: "The folder inside the bucket backups are kept in. Pruning removes only backup files in this folder, never other objects. Empty means the bucket root."},
		{Key: KeyS3PathStyle, Kind: settings.Bool, Default: "true", Owner: h,
			Description: "Address the bucket as <endpoint>/<bucket> (true) rather than <bucket>.<endpoint> (false). Cloudflare R2 and Google Cloud Storage accept both."},
		{Key: KeyS3PartSize, Kind: settings.Int, Default: "16777216", Owner: h,
			Description: "Size in bytes of one part of a multipart upload; a backup no larger than this goes in one request. Amazon S3 needs at least 5242880 (5 MiB) for every part but the last."},
		{Key: KeyS3SealedSecret, Kind: settings.String, Default: "", Owner: h,
			Description: "The bucket's access key id and secret, sealed (encrypted) with this host's master key. Set it with local-server backup-credentials; a value written any other way is refused, so a secret is never stored in plain text."},
		{Key: KeyRetentionCount, Kind: settings.Int, Default: "100", Owner: h,
			Description: "How many of the newest backups are kept; older ones are deleted after each successful backup. 0 turns the count limit off. The newest backup is always kept."},
		{Key: KeyRetentionAge, Kind: settings.Duration, Default: "0s", Owner: h,
			Description: "Backups older than this are deleted after each successful backup. 0 turns the age limit off. The newest backup is always kept."},
		{Key: KeyChunkBytes, Kind: settings.Int, Default: "1048576", Owner: h,
			Description: "Size in bytes of the pieces a backup is encrypted in, so a large database is never held in memory whole. It is recorded in the backup, so a change affects new backups only."},
		{Key: KeyTempDir, Kind: settings.String, Default: "", Owner: h,
			Description: "The folder where a backup's temporary copies are made (a private sub-folder, removed afterwards). Empty means the operating system's temporary folder. It needs room for the database twice."},
		{Key: KeyRunTimeout, Kind: settings.Duration, Default: "1h", Owner: h,
			Description: "Longest one backup, from copying the database to the end of the upload, may take before it is stopped and counted as failed."},
		{Key: KeyNotifyInterval, Kind: settings.Duration, Default: defaultNotifyEvery.String(), Owner: h,
			Description: "While backups keep failing you are notified at the first failure and then at most this often."},
		{Key: KeyLastSuccessAt, Kind: settings.Int, Default: "0", Owner: h,
			Description: "When the last backup finished and was verified, Unix milliseconds UTC; 0 means never. local-server writes it after each backup. Changing it moves the time the next backup is due."},
	}
}

// RegisterSettings registers the backup settings and one minimize switch per table in
// ops_data_catalog, keyed backup.minimize.<table> (req_001 D-64, D-69), with the write
// validators. The protected tables (req_001 D-139) get a switch too, so the refusal can name the
// table and the reason, but its validator refuses every write and the value is never read. db must be open (the catalog is filled at open). It is safe to call on every open.
func RegisterSettings(ctx context.Context, db *storage.DB, st *settings.Store) error {
	if err := st.Register(Defs()...); err != nil {
		return err
	}
	tables, err := catalogTables(ctx, db)
	if err != nil {
		return err
	}
	for _, t := range tables {
		desc := "Backup: whether the rows of table " + t + " are left out of backups (true) or kept as they are (false). A table can be left out only when every table that refers to it is also left out; local-server refuses a change that breaks this and names the tables."
		if IsProtected(t) {
			desc = "Backup: table " + t + " is protected and always included in a backup, because leaving it out would make a restore fail or leave a snapshot that cannot recover this host's keys. This setting cannot be changed: every write is refused and any stored value is ignored."
		}
		err := st.Register(settings.Def{Key: MinimizeKey(t), Kind: settings.Bool, Default: boolText(DefaultMinimize(t)), Owner: settings.Host, Description: desc})
		if err != nil {
			return err
		}
		table := t
		if IsProtected(table) {
			if err := st.SetValidator(MinimizeKey(table), func(context.Context, string) error { return protectedError(table) }); err != nil {
				return err
			}
			continue
		}
		if err := st.SetValidator(MinimizeKey(table), func(ctx context.Context, v string) error {
			return validateMinimizeWrite(ctx, db, st, table, v == "true")
		}); err != nil {
			return err
		}
	}
	return registerValidators(st)
}

func catalogTables(ctx context.Context, db *storage.DB) ([]string, error) {
	rows, err := db.Reader().Query(ctx, `SELECT DISTINCT table_name FROM ops_data_catalog ORDER BY table_name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// minimizeSettings reads the minimize switch of every catalog table. A protected table is always
// kept: its stored value, if an older database holds one, is not read (req_001 D-139).
func minimizeSettings(ctx context.Context, db *storage.DB, st *settings.Store) (map[string]bool, error) {
	tables, err := catalogTables(ctx, db)
	if err != nil {
		return nil, err
	}
	out := make(map[string]bool, len(tables))
	for _, t := range tables {
		if IsProtected(t) {
			out[t] = false
			continue
		}
		v, err := st.Bool(ctx, MinimizeKey(t), "")
		if err != nil {
			return nil, fmt.Errorf("read setting %s: %w", MinimizeKey(t), err)
		}
		out[t] = v
	}
	return out, nil
}

// validateMinimizeWrite refuses a write of table's switch that would break req_001 D-69, naming
// the tables.
func validateMinimizeWrite(ctx context.Context, db *storage.DB, st *settings.Store, table string, value bool) error {
	cur, err := minimizeSettings(ctx, db, st)
	if err != nil {
		return err
	}
	cur[table] = value
	refs, err := db.ForeignKeyReferrers(ctx)
	if err != nil {
		return err
	}
	if merr := CheckMinimize(refs, cur).involving(table); merr != nil {
		return merr
	}
	return nil
}

func registerValidators(st *settings.Store) error {
	positiveDur := func(_ context.Context, v string) error {
		d, err := time.ParseDuration(v)
		if err != nil {
			return err
		}
		if d <= 0 {
			return errors.New("must be greater than 0")
		}
		return nil
	}
	notNegDur := func(_ context.Context, v string) error {
		d, err := time.ParseDuration(v)
		if err != nil {
			return err
		}
		if d < 0 {
			return errors.New("must be at least 0")
		}
		return nil
	}
	intRange := func(lo, hi int64) func(context.Context, string) error {
		return func(_ context.Context, v string) error {
			n, err := parseInt(v)
			if err != nil {
				return err
			}
			if n < lo || (hi > 0 && n > hi) {
				if hi > 0 {
					return fmt.Errorf("must be between %d and %d", lo, hi)
				}
				return fmt.Errorf("must be at least %d", lo)
			}
			return nil
		}
	}
	checks := map[string]func(context.Context, string) error{
		KeyInterval:       positiveDur,
		KeyRunTimeout:     positiveDur,
		KeyNotifyInterval: positiveDur,
		KeyRetentionAge:   notNegDur,
		KeyRetentionCount: intRange(0, 0),
		KeyLastSuccessAt:  intRange(0, 0),
		KeyS3PartSize:     intRange(1, 0),
		KeyChunkBytes:     intRange(1, maxChunkSize),
		KeyTarget: func(_ context.Context, v string) error {
			if v != TargetFolder && v != TargetS3 {
				return fmt.Errorf("must be %s or %s", TargetS3, TargetFolder)
			}
			return nil
		},
		KeyS3Endpoint: func(_ context.Context, v string) error {
			if v == "" {
				return nil
			}
			u, err := url.Parse(v)
			if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
				return errors.New("must be an http:// or https:// address with a host")
			}
			if u.User != nil {
				return errors.New("must not carry a user name or password; the access key is stored sealed")
			}
			return nil
		},
		KeyS3SealedSecret: func(_ context.Context, v string) error {
			if v != "" && !looksSealed(v) {
				return errors.New("must be a sealed value; set the bucket access key with local-server backup-credentials")
			}
			return nil
		},
	}
	keys := make([]string, 0, len(checks))
	for k := range checks {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if err := st.SetValidator(k, checks[k]); err != nil {
			return err
		}
	}
	return nil
}

// Config is the backup configuration as the settings hold it now, apart from the bucket
// credentials, which LoadCredentials reads when a bucket target is used.
type Config struct {
	Enabled        bool
	Interval       time.Duration
	Target         TargetConfig // Kind, Folder and S3 without Credentials
	ChunkBytes     int
	TempDir        string
	RunTimeout     time.Duration
	NotifyInterval time.Duration
	RetentionCount int64
	RetentionAge   time.Duration
	LastSuccess    time.Time // zero when never
	Minimize       map[string]bool
}

// MinimizedTables returns the tables set to minimize, sorted.
func (c Config) MinimizedTables() []string {
	var out []string
	for t, m := range c.Minimize {
		if m {
			out = append(out, t)
		}
	}
	sort.Strings(out)
	return out
}

// LoadConfig reads the backup settings.
func LoadConfig(ctx context.Context, db *storage.DB, st *settings.Store) (Config, error) {
	var c Config
	var err error
	str := func(key string) string {
		if err != nil {
			return ""
		}
		var v string
		v, err = st.String(ctx, key, "")
		if err != nil {
			err = fmt.Errorf("read setting %s: %w", key, err)
		}
		return v
	}
	boolean := func(key string) bool {
		if err != nil {
			return false
		}
		var v bool
		v, err = st.Bool(ctx, key, "")
		if err != nil {
			err = fmt.Errorf("read setting %s: %w", key, err)
		}
		return v
	}
	num := func(key string) int64 {
		if err != nil {
			return 0
		}
		var v int64
		v, err = st.Int(ctx, key, "")
		if err != nil {
			err = fmt.Errorf("read setting %s: %w", key, err)
		}
		return v
	}
	dur := func(key string) time.Duration {
		if err != nil {
			return 0
		}
		var v time.Duration
		v, err = st.Duration(ctx, key, "")
		if err != nil {
			err = fmt.Errorf("read setting %s: %w", key, err)
		}
		return v
	}
	c.Enabled = boolean(KeyEnabled)
	c.Interval = dur(KeyInterval)
	c.Target.Kind = str(KeyTarget)
	c.Target.Folder = str(KeyFolderPath)
	c.Target.S3.Endpoint = str(KeyS3Endpoint)
	c.Target.S3.Region = str(KeyS3Region)
	c.Target.S3.Bucket = str(KeyS3Bucket)
	c.Target.S3.Prefix = str(KeyS3Prefix)
	c.Target.S3.PathStyle = boolean(KeyS3PathStyle)
	c.Target.S3.PartSize = num(KeyS3PartSize)
	c.ChunkBytes = int(num(KeyChunkBytes))
	c.TempDir = str(KeyTempDir)
	c.RunTimeout = dur(KeyRunTimeout)
	c.NotifyInterval = dur(KeyNotifyInterval)
	c.RetentionCount = num(KeyRetentionCount)
	c.RetentionAge = dur(KeyRetentionAge)
	if ms := num(KeyLastSuccessAt); ms > 0 {
		c.LastSuccess = time.UnixMilli(ms).UTC()
	}
	if err != nil {
		return Config{}, err
	}
	if c.Minimize, err = minimizeSettings(ctx, db, st); err != nil {
		return Config{}, err
	}
	c.Target.S3.Prefix = strings.TrimLeft(c.Target.S3.Prefix, "/")
	return c, nil
}

func parseInt(v string) (int64, error) { return strconv.ParseInt(v, 10, 64) }

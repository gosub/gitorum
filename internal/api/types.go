package api

// ---- response types --------------------------------------------------------

type CategoriesResponse struct {
	Categories []CategorySummary `json:"categories"`
}

type CategorySummary struct {
	Slug        string `json:"slug"`
	Name        string `json:"name"`
	Description string `json:"description"`
	ThreadCount int    `json:"thread_count"`
}

type ThreadsResponse struct {
	Category     string          `json:"category"`      // slug
	CategoryName string          `json:"category_name"` // display name
	Threads      []ThreadSummary `json:"threads"`
}

type ThreadSummary struct {
	Slug        string `json:"slug"`
	Title       string `json:"title"`
	Author      string `json:"author"`
	ReplyCount  int    `json:"reply_count"`
	CreatedAt   string `json:"created_at"`
	Hidden      bool   `json:"hidden,omitempty"` // root post tombstoned
	LastReplyAt string `json:"last_reply_at"`
}

type ThreadResponse struct {
	Category     string         `json:"category"`
	CategoryName string         `json:"category_name"`
	Slug         string         `json:"slug"`
	Title        string         `json:"title"`
	Posts        []PostResponse `json:"posts"`
}

// PostResponse is the wire representation of a post sent to the browser.
// SigStatus is "valid", "untrusted", "invalid", "missing", or "deleted"
// (tombstoned).
type PostResponse struct {
	Author     string `json:"author"`
	PubKey     string `json:"pubkey"`
	Timestamp  string `json:"timestamp"`
	Title      string `json:"title,omitempty"`
	Parent     string `json:"parent"` // hash of the post this one answers
	Hash       string `json:"hash"`   // hash of this post's file (what replies put in parent)
	Body       string `json:"body"`
	BodyHTML   string `json:"body_html"`
	Filename   string `json:"filename"`
	SigStatus  string `json:"sig_status"`
	SigError   string `json:"sig_error,omitempty"`
	Tombstoned bool   `json:"tombstoned,omitempty"`
}

type StatusResponse struct {
	Username    string `json:"username"`
	PubKey      string `json:"pubkey"`
	IsAdmin     bool   `json:"is_admin"`
	ForumName   string `json:"forum_name"`
	RemoteURL   string `json:"remote_url,omitempty"`
	Synced      bool   `json:"synced"`
	Ahead       int    `json:"ahead"`  // local commits not yet pushed
	Behind      int    `json:"behind"` // fetched remote commits not yet integrated
	Initialized bool   `json:"initialized"`
	// AdminKeyMismatch is set when GITORUM.toml names an admin key other
	// than the one pinned locally (the pinned one stays authoritative).
	AdminKeyMismatch bool   `json:"admin_key_mismatch,omitempty"` // false until forum repo exists
	LastSyncAt       string `json:"last_sync_at,omitempty"`       // RFC3339, set after first sync
}

type SetupRequest struct {
	Username    string `json:"username"`
	ForumName   string `json:"forum_name"`
	Description string `json:"description"`
	RemoteURL   string `json:"remote_url"`
}

// ---- request types ---------------------------------------------------------

type ReplyRequest struct {
	Body    string `json:"body"`
	ReplyTo string `json:"reply_to"` // filename of the post answered; "" = root
}

type NewThreadRequest struct {
	Category string `json:"category"`
	Title    string `json:"title"`
	Slug     string `json:"slug"` // optional; derived from the title when empty
	Body     string `json:"body"`
}

type NewThreadResponse struct {
	OKResponse
	Slug string `json:"slug"`
}

type AdminDeleteRequest struct {
	Category string `json:"category"`
	Thread   string `json:"thread"`
	Filename string `json:"filename"`
	Purge    bool   `json:"purge"` // also remove the post file from the tree
}

type AdminAddKeyRequest struct {
	Username string `json:"username"`
	PubKey   string `json:"pubkey"`
	Rotate   bool   `json:"rotate"` // append to an existing user's keys
	Token    string `json:"token"`  // signed join token; replaces username/pubkey
}

type CreateCategoryRequest struct {
	Slug        string `json:"slug"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

type JoinRequestsResponse struct {
	Requests []JoinRequestSummary `json:"requests"`
}

type JoinRequestSummary struct {
	Username string `json:"username"`
	PubKey   string `json:"pubkey"`
	Problem  string `json:"problem,omitempty"` // proof of key possession missing/invalid
}

type ApproveRejectRequest struct {
	Username string `json:"username"`
}

// ---- generic ---------------------------------------------------------------

type OKResponse struct {
	OK      bool   `json:"ok"`
	Pushed  bool   `json:"pushed"` // false when a write was committed locally but not pushed
	Message string `json:"message,omitempty"`
}

type SyncResponse struct {
	OK      bool `json:"ok"`
	Pulled  int  `json:"pulled"`
	Rebased int  `json:"rebased"`
	Pushed  bool `json:"pushed"`
}

type ErrorResponse struct {
	Error string `json:"error"`
}

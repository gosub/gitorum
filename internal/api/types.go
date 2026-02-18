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
	LastReplyAt string `json:"last_reply_at"`
}

type ThreadResponse struct {
	Category string         `json:"category"`
	Slug     string         `json:"slug"`
	Posts    []PostResponse `json:"posts"`
}

// PostResponse is the wire representation of a post sent to the browser.
// SigStatus is "valid", "invalid", "missing", or "deleted" (tombstoned).
type PostResponse struct {
	Author     string `json:"author"`
	PubKey     string `json:"pubkey"`
	Timestamp  string `json:"timestamp"`
	Parent     string `json:"parent"`
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
	Initialized bool   `json:"initialized"`              // false until forum repo exists
	LastSyncAt  string `json:"last_sync_at,omitempty"` // RFC3339, set after first sync
}

type SetupRequest struct {
	Username  string `json:"username"`
	ForumName string `json:"forum_name"`
	RemoteURL string `json:"remote_url"`
}

// ---- request types ---------------------------------------------------------

type ReplyRequest struct {
	Body string `json:"body"`
}

type NewThreadRequest struct {
	Category string `json:"category"`
	Slug     string `json:"slug"`
	Body     string `json:"body"`
}

type AdminDeleteRequest struct {
	Category string `json:"category"`
	Thread   string `json:"thread"`
	Filename string `json:"filename"`
}

type AdminAddKeyRequest struct {
	Username string `json:"username"`
	PubKey   string `json:"pubkey"`
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
}

type ApproveRejectRequest struct {
	Username string `json:"username"`
}

// ---- generic ---------------------------------------------------------------

type OKResponse struct {
	OK      bool   `json:"ok"`
	Message string `json:"message,omitempty"`
}

type ErrorResponse struct {
	Error string `json:"error"`
}

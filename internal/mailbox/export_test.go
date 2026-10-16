package mailbox

// For tests in package mailbox_test, which may import packages that import
// mailbox.
var (
	NewEmptySession = newEmptySession
	AppendAt        = appendAt
)

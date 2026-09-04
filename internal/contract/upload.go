package contract

// MaxTodoImageBytes is part of the upload contract shared by transports and
// persistence validation. Keeping it here prevents the HTTP edge from importing
// a storage package merely to enforce the same request limit.
const MaxTodoImageBytes = int64(10 * 1024 * 1024)

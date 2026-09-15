// Package expense reserves a boundary for database-independent expense rules.
// Current draft, confirmation, revision and deletion operations are transactional
// and implemented in internal/postgres/expenses.go.
package expense

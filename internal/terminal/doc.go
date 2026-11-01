// Package terminal spawns and owns PTYs. Output is a raw byte stream, not events: it is too
// large and frequent to store or fan out like them.
package terminal

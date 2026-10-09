//go:build soak && darwin

package engine

// rssUnit converts getrusage's ru_maxrss to bytes: macOS reports bytes.
const rssUnit = 1

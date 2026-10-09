//go:build soak && linux

package engine

// rssUnit converts getrusage's ru_maxrss to bytes: Linux reports kilobytes.
const rssUnit = 1024

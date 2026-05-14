// Package outputcache mirrors the API of go-router/outputcache for minmux.
// It provides HTTP response caching with per-route opt-in, cache profiles,
// flexible cache key derivation (path, query, header, custom), tags for
// group invalidation, sliding expiration, and ETag-based revalidation.
//
// Routes opt in to caching via the WithOutputCache or WithCacheProfile
// router options. The Cache.Middleware() is registered globally; it does
// nothing for routes that have not opted in.
//
// Storage backends live in sibling modules:
//
//   - github.com/joakimcarlsson/minmux/outputcache/inmemory — process-local
//   - github.com/joakimcarlsson/minmux/outputcache/redis    — Redis-backed
package outputcache

package daemon

// gitCACertSetter is implemented by repocache.Cache. It is optional so test
// doubles of repoCacheBackend do not have to implement it.
type gitCACertSetter interface {
	SetExtraCACerts(pemCerts string) error
}

// applyGitCACerts hands the server's extra CA certificates to the repo cache,
// which makes git trust them for clone, fetch, and agents' push. It runs on
// registration and on every workspace repo refresh (which precedes each
// checkout), so a CA rotated on the server reaches the daemon without a
// restart. An empty value removes a previously applied bundle.
func (d *Daemon) applyGitCACerts(pemCerts string) {
	cache, ok := d.repoCache.(gitCACertSetter)
	if !ok {
		return
	}
	err := cache.SetExtraCACerts(pemCerts)
	msg := ""
	if err != nil {
		msg = err.Error()
	}
	if prev := d.gitCALastErr.Swap(&msg); err != nil && (prev == nil || *prev != msg) {
		d.logger.Warn("git CA: could not apply server CA certificates; repositories on hosts with a private CA may fail to sync", "error", err)
	}
}

Exploratory runs of the v0.2.0 optimisation round (13.10.2026), kept for
traceability of DESIGN_NOTES.md section 7.6. Single 8-10 s runs made with
goarena-bench on the same machine while the code was being changed; they
are NOT part of the reported experiment matrix.

zipf5M_rwmutex.json, uniform10M_rwmutex.json      seqlock reads + sync.RWMutex writer lock
zipf5M_spinlock.json, uniform10M_spinlock.json    seqlock reads + spinning TTAS writer lock
zipf5M_mutex_adopted.json, uniform1M_mutex_adopted.json   adopted variant (sync.Mutex + reader wait)
uniform1M_mutex_adopted_g14.json, uniform1M_gobytes_g14.json   the same with 14 goroutines
*_reference.json                                  baselines run in the same period

aborted_spinlock_session/  first 16 E1 runs (and partial micro-benchmarks) of the
                           v2 session with the spinning lock; the session was
                           aborted when the >1 ms tail regression was noticed
                           (113-131 sampled operations above 1 ms per run versus
                           0 in v0.1.0) and restarted with the adopted lock.

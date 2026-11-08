import { useEffect, useState } from 'react'
import { Link, useParams } from 'react-router-dom'
import { api, type Candidate } from '../api'
import { money } from '../format'
import { useLayout } from '../Layout'
import { ProjectTabs } from '../ProjectTabs'
import { Alert } from '../ui'
import { errorText, useLoad } from '../useLoad'

type Filter = 'all' | 'candidate' | 'selected' | 'rejected'
const filters: { key: Filter; label: string }[] = [
  { key: 'all', label: 'All' },
  { key: 'candidate', label: 'To review' },
  { key: 'selected', label: 'Selected' },
  { key: 'rejected', label: 'Rejected' },
]

export function ReviewPage() {
  const { code = '' } = useParams()
  const { refreshNav } = useLayout()
  const { data, error, reload } = useLoad(() => api.candidates(code), [code])
  const [activeId, setActiveId] = useState<number | null>(null)
  const [filter, setFilter] = useState<Filter>('all')
  const [text, setText] = useState('')
  const [screen, setScreen] = useState('')
  const [busy, setBusy] = useState(false)
  const [actionError, setActionError] = useState('')

  const items = data?.items ?? []
  const active = items.find((c) => c.id === activeId) ?? items.find((c) => c.status === 'candidate') ?? items[0]

  useEffect(() => {
    setText(active?.text ?? '')
    setScreen(active?.onScreenText ?? '')
  }, [active?.id, active?.text, active?.onScreenText])

  if (error) return <Alert title="Could not load candidates">{error.message}</Alert>
  if (!data) return null

  const count = (s: Candidate['status']) => items.filter((c) => c.status === s).length
  const selected = count('selected') + count('exported')
  const rejected = count('rejected')
  const left = count('candidate')
  const pos = active ? items.indexOf(active) : -1
  const go = (step: number) => items.length && setActiveId(items[(pos + step + items.length) % items.length].id)

  async function act(fn: () => Promise<void>, moveNext: boolean) {
    if (!active) return
    setBusy(true)
    setActionError('')
    try {
      await fn()
      if (moveNext) {
        const next = items.find((c, i) => i > pos && c.status === 'candidate' && c.id !== active.id)
        if (next) setActiveId(next.id)
      }
      reload()
      refreshNav()
    } catch (e) {
      setActionError(errorText(e))
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="page">
      <header style={{ display: 'flex', flexDirection: 'column', gap: 18 }}>
        <h1>{data.code} · <span className="grad">PICK THE KEEPERS</span></h1>
        <ProjectTabs code={data.code} open={left} />
      </header>

      {items.length === 0 ? (
        <div className="card empty">No candidates yet. <Link to={`/projects/${code}/generate`}>Generate a batch.</Link></div>
      ) : (
        <div className="split">
          <section aria-label="Candidates" className="card side-col" style={{ overflow: 'hidden' }}>
            <div style={{ display: 'flex', flexDirection: 'column', gap: 12, padding: '16px 18px', borderBottom: '1px solid var(--line-soft)' }}>
              <div role="img" aria-label={`${selected} selected, ${rejected} rejected, ${left} left`} className="bar" style={{ height: 6 }}>
                <span style={{ flex: `${selected} 1 0`, background: '#a78bfa', borderRadius: 999 }} />
                <span style={{ flex: `${rejected} 1 0`, background: '#4a4a56', borderRadius: 999 }} />
                <span style={{ flex: `${left} 1 0`, background: 'rgba(255,255,255,0.08)', borderRadius: 999 }} />
              </div>
              <div role="group" aria-label="Filter" className="row" style={{ gap: 6 }}>
                {filters.map((f) => <button key={f.key} type="button" className={'pill sm' + (filter === f.key ? ' on' : '')} aria-pressed={filter === f.key} onClick={() => setFilter(f.key)}>{f.label}</button>)}
              </div>
            </div>
            {items.filter((c) => filter === 'all' || c.status === filter || (filter === 'selected' && c.status === 'exported')).map((c) => (
              <button key={c.id} type="button" className={'item' + (c.id === active?.id ? ' active' : '')} aria-current={c.id === active?.id} onClick={() => setActiveId(c.id)}>
                <span className="dot" aria-hidden="true" style={{ background: c.status === 'candidate' ? '#f4f3f8' : c.status === 'rejected' ? '#4a4a56' : '#a78bfa' }} />
                <span style={{ display: 'flex', flexDirection: 'column', gap: 4, minWidth: 0 }}>
                  <span style={c.status === 'rejected' ? { color: '#8e8d9c', textDecoration: 'line-through' } : undefined}>{c.text}</span>
                  <span className="muted" style={{ fontSize: 12 }}>{[c.number, c.type, c.basedOn || 'new'].filter(Boolean).join(' · ')}</span>
                </span>
              </button>
            ))}
            {data.dropped.length > 0 && <p className="muted" style={{ margin: 0, padding: '12px 18px', fontSize: 12 }} title={data.dropped.map((d) => d.text).join('\n')}>{data.dropped.length} duplicates hidden</p>}
          </section>

          {active && (
            <section aria-labelledby="cand-title" className="card grow" style={{ display: 'flex', flexDirection: 'column', gap: 18, padding: 24 }}>
              <div className="header" style={{ gap: 10 }}>
                <h2 id="cand-title" className="section">CANDIDATE {pos + 1} / {items.length}</h2>
                <div className="row" style={{ gap: 6 }}>
                  <span className={'chip ' + (active.basedOn ? 'tier-reference' : 'mode-explore')}>{active.basedOn ? `Like ${active.basedOn}` : 'New type'}</span>
                  {active.type && <span className="chip tier-good">{active.type}</span>}
                </div>
              </div>

              {actionError && <Alert title="That did not work">{actionError}</Alert>}

              <div className="field">
                <label htmlFor="hook-text" className="lbl">HOOK</label>
                <textarea id="hook-text" className="input" rows={2} maxLength={300} style={{ fontSize: 18, fontWeight: 700 }} value={text} readOnly={active.status !== 'candidate'} onChange={(e) => setText(e.target.value)} />
              </div>
              <div className="field">
                <label htmlFor="screen-text" className="lbl">ON SCREEN</label>
                <input id="screen-text" className="input" maxLength={120} value={screen} readOnly={active.status !== 'candidate'} onChange={(e) => setScreen(e.target.value)} />
              </div>

              <div className="row" style={{ justifyContent: 'space-between', padding: '14px 16px', borderRadius: 16, background: 'rgba(255,255,255,0.03)', fontSize: 13 }}>
                <span className="muted">{active.rationale || (active.basedOn ? `Based on ${active.basedOn}` : 'New type')}</span>
                {active.basedOnCpaCents != null && <span className="strong">{active.basedOn} · CPA {money(active.basedOnCpaCents)}</span>}
              </div>

              {active.number && (
                <div className="banner">
                  <span style={{ fontWeight: 800, color: '#ede9fe' }}>{active.number}</span>
                  <code style={{ color: '#ddd6fe' }}>{active.adName}</code>
                </div>
              )}

              <div className="header" style={{ marginTop: 'auto', gap: 10 }}>
                <div className="row" style={{ gap: 8 }}>
                  {active.status === 'candidate' && (
                    <>
                      <button type="button" className="btn btn-p" disabled={busy || !text.trim()} onClick={() => act(() => api.select(active.id, { text, onScreenText: screen }), true)}>Select as {data.nextNumber}</button>
                      <button type="button" className="btn btn-g" disabled={busy} onClick={() => act(() => api.reject(active.id), true)}>Reject</button>
                    </>
                  )}
                  {(active.status === 'selected' || active.status === 'rejected') && (
                    <button type="button" className="btn btn-g" disabled={busy} onClick={() => act(() => api.undo(active.id), false)}>Undo</button>
                  )}
                  {active.status === 'exported' && <span className="muted">Exported to production</span>}
                </div>
                <div className="row" style={{ gap: 6 }}>
                  <button type="button" className="btn btn-g" style={{ padding: '0 16px' }} aria-label="Previous candidate" onClick={() => go(-1)}>←</button>
                  <button type="button" className="btn btn-g" style={{ padding: '0 16px' }} aria-label="Next candidate" onClick={() => go(1)}>→</button>
                </div>
              </div>
            </section>
          )}
        </div>
      )}
    </div>
  )
}

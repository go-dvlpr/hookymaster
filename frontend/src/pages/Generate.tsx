import { useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'
import { api } from '../api'
import { money } from '../format'
import { useLayout } from '../Layout'
import { ProjectTabs } from '../ProjectTabs'
import { Alert, FieldError, Help } from '../ui'
import { errorText, useLoad } from '../useLoad'

const sizes = [10, 20, 30, 50, 100]
const maxBatch = 100

export function GeneratePage() {
  const { code = '' } = useParams()
  const navigate = useNavigate()
  const { refreshNav } = useLayout()
  const opts = useLoad(() => api.generateOptions(code), [code])
  const project = useLoad(() => api.project(code), [code])
  const [count, setCount] = useState('20')
  const [existingPct, setExistingPct] = useState(80)
  const [shares, setShares] = useState<Record<string, string>>({})
  const [brief, setBrief] = useState('')
  const [briefError, setBriefError] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')

  if (opts.error) return <Alert title="Could not load the generator">{opts.error.message}</Alert>
  if (!opts.data || !project.data) return null
  const o = opts.data
  const st = o.settings

  const n = Number(count)
  const countError = Number.isInteger(n) && n >= 1 && n <= maxBatch ? '' : `Enter a whole number from 1 to ${maxBatch}.`
  const size = countError ? 0 : n
  const refs = o.references
  const hasRefs = refs.length > 0
  const effectivePct = hasRefs ? existingPct : 0
  const existingN = Math.round((size * effectivePct) / 100)
  const shareValues = Object.fromEntries(Object.entries(shares).filter(([, v]) => v !== '').map(([k, v]) => [k, Number(v)]))
  const shareTotal = Object.values(shareValues).reduce((a, b) => a + b, 0)
  const shareError = shareTotal > 100 ? `Shares add up to ${shareTotal}%. Keep them at 100% or less.` : ''
  const typeName = (c: string) => o.types.find((t) => t.code === c)?.name ?? c
  const picked = Object.keys(shares)
  const typeSummary = picked.length === 0
    ? 'model decides'
    : picked.map((c) => `${typeName(c)} ${shares[c] || '—'}%`).join(', ')

  async function generate() {
    if (countError || shareError) return
    if (o.needsBrief && !brief.trim()) {
      setBriefError('Describe what you sell, to whom and which angle the hooks should take.')
      return
    }
    setBusy(true)
    setError('')
    try {
      await api.generate(code, { n: size, exploitPct: effectivePct, typeShares: shareValues, brief })
      refreshNav()
      navigate(`/projects/${code}/candidates`)
    } catch (e) {
      setError(errorText(e))
      setBusy(false)
    }
  }

  return (
    <div className="page">
      <header style={{ display: 'flex', flexDirection: 'column', gap: 18 }}>
        <h1>{code} · <span className="grad">NEXT BATCH</span></h1>
        <ProjectTabs code={code} open={project.data.openCandidates} />
      </header>

      {!o.llmReady && <Alert title="Generation is off">Add OPENAI_API_KEY to the .env file and restart the app.</Alert>}
      {error && <Alert title="The batch was not generated">{error}</Alert>}

      <div className="split">
        <form aria-label="Batch settings" className="card grow" style={{ display: 'flex', flexDirection: 'column', gap: 26, padding: 24 }} onSubmit={(e) => e.preventDefault()} noValidate>
          <div className="field" style={{ gap: 10 }}>
            <span className="lbl-row">
              <label htmlFor="count" className="lbl">CANDIDATES</label>
              <Help>How many new hooks to write. Up to {maxBatch}. Batches above 25 are written in parallel parts, so they take about as long as a small one.</Help>
            </span>
            <div className="row" style={{ gap: 8 }}>
              <input id="count" className="input" type="number" min={1} max={maxBatch} inputMode="numeric" style={{ width: 96, textAlign: 'center' }}
                value={count} aria-invalid={!!countError} aria-describedby="count-error" onChange={(e) => setCount(e.target.value)} />
              {sizes.map((v) => <button key={v} type="button" className={'pill' + (n === v ? ' on' : '')} aria-pressed={n === v} onClick={() => setCount(String(v))}>{v}</button>)}
            </div>
            <FieldError id="count-error">{countError}</FieldError>
          </div>

          <div className="field" style={{ gap: 12 }}>
            <span className="lbl-row">
              <label htmlFor="split" className="lbl">EXISTING · NEW</label>
              <Help>
                Existing: variations of the reference hooks. They keep the angle and rhythm and change the words.
                New: fresh angles that do not reuse any hook of the project.
                {!hasRefs && ' This project has no hooks with text yet, so the whole batch is new.'}
              </Help>
            </span>
            <div className="row" style={{ justifyContent: 'space-between', fontWeight: 800 }}>
              <span style={{ color: '#ddd6fe' }}>{effectivePct}% · {existingN}</span>
              <span style={{ color: '#7dd3fc' }}>{size - existingN} · {100 - effectivePct}%</span>
            </div>
            <input id="split" type="range" min={0} max={100} step={10} value={effectivePct} disabled={!hasRefs} onChange={(e) => setExistingPct(Number(e.target.value))} />
          </div>

          <details>
            <summary style={{ cursor: 'pointer' }}>
              <span className="lbl">TYPE MIX</span> <span className="hint">· {typeSummary}</span>
            </summary>
            <div style={{ display: 'flex', flexDirection: 'column', gap: 10, marginTop: 12 }}>
              <span className="hint">Percent of the whole batch per type. Types you do not pick are left to the model.</span>
              {picked.map((c) => (
                <div key={c} className="row" style={{ flexWrap: 'nowrap', gap: 8 }}>
                  <label htmlFor={`share-${c}`} style={{ flex: 1, fontWeight: 600 }}>{typeName(c)}</label>
                  <input id={`share-${c}`} className="input" type="number" min={0} max={100} inputMode="numeric" placeholder="%"
                    style={{ width: 80, minHeight: 36, padding: '4px 10px', textAlign: 'right' }} aria-invalid={!!shareError} aria-describedby="share-error"
                    value={shares[c]} onChange={(e) => setShares({ ...shares, [c]: e.target.value })} />
                  <span className="hint">%</span>
                  <button type="button" className="link-btn" aria-label={`Remove ${typeName(c)}`}
                    onClick={() => { const next = { ...shares }; delete next[c]; setShares(next) }}>×</button>
                </div>
              ))}
              <FieldError id="share-error">{shareError}</FieldError>
              <div className="row" style={{ gap: 6 }}>
                {o.types.filter((t) => !(t.code in shares)).map((t) => (
                  <button key={t.code} type="button" className="pill sm"
                    onClick={() => setShares({ ...shares, [t.code]: '' })}>+ {t.name}</button>
                ))}
              </div>
            </div>
          </details>

          {o.needsBrief ? (
            <div className="field" style={{ gap: 10 }}>
              <label htmlFor="brief" className="lbl">BRIEF · REQUIRED</label>
              <span className="hint">The project has no description and no hooks yet. Tell the model what you sell, to whom and which angle to take, or add a description in Settings.</span>
              <textarea id="brief" className="input" rows={4} maxLength={1000} aria-invalid={!!briefError} aria-describedby="brief-error"
                placeholder="e.g. A meal-prep app for busy parents. Hooks about saving time on weekday dinners."
                value={brief} onChange={(e) => { setBrief(e.target.value); setBriefError('') }} />
              <FieldError id="brief-error">{briefError}</FieldError>
            </div>
          ) : (
            <details>
              <summary style={{ cursor: 'pointer' }}><span className="lbl">BRIEF</span> <span className="hint">· optional</span></summary>
              <textarea className="input" aria-label="Brief" rows={2} maxLength={1000} style={{ marginTop: 10 }} placeholder="e.g. more hooks for parents" value={brief} onChange={(e) => setBrief(e.target.value)} />
            </details>
          )}
        </form>

        <aside aria-labelledby="plan-title" className="card side-col" style={{ display: 'flex', flexDirection: 'column', gap: 16, padding: 24, background: 'linear-gradient(160deg,#1b1440,#16161d 60%)', borderColor: 'rgba(167,139,250,0.25)' }}>
          <h2 id="plan-title" className="section">BUILT ON · {refs.length}</h2>
          <span className="hint">
            Top {st.refSharePct}% of hooks with CPA up to {money(st.maxCpaCents)}, {st.minRefs}–{st.maxRefs} hooks. <Link to={`/projects/${code}/settings`}>Settings</Link>
          </span>
          <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
            {refs.length === 0 && <span className="muted">No references yet.</span>}
            {refs.map((r) => (
              <div key={r.number} className="row" style={{ justifyContent: 'space-between', flexWrap: 'nowrap', padding: '10px 14px', borderRadius: 12, background: 'rgba(139,92,246,0.12)' }}>
                <span style={{ minWidth: 0, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }} title={r.text}><b>{r.number}</b> <span style={{ color: '#d4d3de' }}>{r.text}</span></span>
                {r.cpaCents > 0 && <span className="strong" style={{ fontSize: 13 }}>{money(r.cpaCents)}</span>}
              </div>
            ))}
          </div>
          {o.antiExamples.length > 0 && (
            <div className="row" style={{ justifyContent: 'space-between', fontSize: 13 }}>
              <span style={{ color: '#fdba74', fontWeight: 700 }}>Avoid</span>
              <span style={{ color: '#d4d3de' }} title={o.antiExamples.map((a) => `${a.number} ${money(a.cpaCents)}`).join(', ')}>{o.antiExamples.map((a) => a.number).join(' · ')}</span>
            </div>
          )}
          <button type="button" className="btn btn-p wide" style={{ minHeight: 48 }} disabled={busy || !o.llmReady || !!shareError || !!countError} onClick={generate}>
            {busy ? 'Generating… up to a minute' : `Generate ${size || ''}`}
          </button>
        </aside>
      </div>
    </div>
  )
}

import { useState, type FormEvent } from 'react'
import { Link, useParams, useSearchParams } from 'react-router-dom'
import { api, type HookRow, type ProjectView } from '../api'
import { int, money, pct, plural, tierLabel } from '../format'
import { useLayout } from '../Layout'
import { ProjectTabs } from '../ProjectTabs'
import { Alert, FieldError } from '../ui'
import { errorText, useLoad } from '../useLoad'

type Filter = 'all' | 'reference' | 'anti' | 'notext' | 'pending'

const filters: { key: Filter; label: string }[] = [
  { key: 'all', label: 'All' },
  { key: 'reference', label: 'References' },
  { key: 'anti', label: 'Anti-examples' },
  { key: 'notext', label: 'No text' },
  { key: 'pending', label: 'No results' },
]

const matches = (h: HookRow, f: Filter) =>
  f === 'all' || (f === 'notext' ? h.text === '' : h.tier === f)

export function ProjectPage() {
  const { code = '' } = useParams()
  const [params, setParams] = useSearchParams()
  const filter = (params.get('filter') as Filter) || 'all'
  const { data, error, reload } = useLoad(() => api.project(code), [code])
  const [editing, setEditing] = useState<number | null>(null)
  const [checked, setChecked] = useState<Set<number>>(new Set())
  const [confirmBulk, setConfirmBulk] = useState(false)
  const [bulkBusy, setBulkBusy] = useState(false)
  const [bulkError, setBulkError] = useState('')
  const { refreshNav } = useLayout()

  if (error) return <Alert title="Could not load the project">{error.message}</Alert>
  if (!data) return null

  const missing = data.hooks.filter((h) => h.text === '').length
  const rows = data.hooks.filter((h) => matches(h, filter))
  const maxTypeCpa = Math.max(data.settings.maxCpaCents * 1.3, ...data.types.map((t) => t.cpaCents))
  const setFilter = (f: Filter) => setParams(f === 'all' ? {} : { filter: f })
  // Only rows on screen can be deleted, so a filter change never deletes hidden hooks.
  const selected = rows.filter((h) => checked.has(h.id))
  const allChecked = selected.length === rows.length && rows.length > 0

  function toggle(id: number) {
    const next = new Set(checked)
    if (next.has(id)) next.delete(id)
    else next.add(id)
    setChecked(next)
    setConfirmBulk(false)
  }

  function toggleAll() {
    setChecked(allChecked ? new Set() : new Set(rows.map((h) => h.id)))
    setConfirmBulk(false)
  }

  async function deleteSelected() {
    setBulkBusy(true)
    setBulkError('')
    try {
      await api.deleteHooks(code, selected.map((h) => h.id))
      setChecked(new Set())
      setConfirmBulk(false)
      reload()
      refreshNav()
    } catch (err) {
      setBulkError(errorText(err))
    } finally {
      setBulkBusy(false)
    }
  }

  return (
    <div className="page">
      <header className="header">
        <h1>{data.code} · <span className="grad">{data.name.toUpperCase()}</span></h1>
        <Link to={`/projects/${code}/generate`} className="btn btn-p">Generate</Link>
      </header>
      <ProjectTabs code={data.code} open={data.openCandidates} />

      {missing > 0 && filter !== 'notext' && (
        <Alert kind="info" title={`${plural(missing, 'hook has', 'hooks have')} no text yet`}
          action={<button type="button" className="btn btn-g" style={{ minHeight: 36 }} onClick={() => setFilter('notext')}>Fill in</button>}>
          They came from the Meta file. Add the text so the generator can learn from them.
        </Alert>
      )}

      <section aria-label="Totals" className="kpis">
        <div className="card kpi"><span className="k">Spend</span><span className="v">{money(data.spendCents, 0)}</span></div>
        <div className="card kpi"><span className="k">Avg CPA</span><span className="v">{money(data.avgCpaCents)}</span></div>
        <div className="card kpi"><span className="k">Hook rate</span><span className="v">{pct(data.hookRate)}</span></div>
        <div className="card kpi"><span className="k">Ranked</span><span className="v">{data.ranked} <span className="muted" style={{ fontSize: 15 }}>/ {data.total}</span></span></div>
      </section>

      <AddHook code={code} pattern={data.adNamePattern} onAdded={reload} />

      <section aria-labelledby="rank-title" style={{ display: 'flex', flexDirection: 'column', gap: 14 }}>
        <div className="header">
          <h2 id="rank-title" className="section">HOOKS</h2>
          <div role="group" aria-label="Filter" className="row" style={{ gap: 6 }}>
            {filters.map((f) => (
              <button key={f.key} type="button" className={'pill sm' + (filter === f.key ? ' on' : '')} aria-pressed={filter === f.key} onClick={() => setFilter(f.key)}>{f.label}</button>
            ))}
          </div>
        </div>
        {selected.length > 0 && (
          <div className="card row" role="region" aria-label="Selected hooks" style={{ justifyContent: 'space-between', padding: '10px 16px', gap: 12 }}>
            <span className="row" style={{ gap: 12 }}>
              <span className="strong">{plural(selected.length, 'hook', 'hooks')} selected</span>
              <button type="button" className="link-btn" onClick={() => { setChecked(new Set()); setConfirmBulk(false) }}>Clear</button>
            </span>
            {confirmBulk ? (
              <span className="row" style={{ gap: 8 }}>
                <span className="muted">Delete {plural(selected.length, 'hook', 'hooks')} with their results?</span>
                <button type="button" className="btn btn-q" onClick={() => setConfirmBulk(false)}>Keep</button>
                <button type="button" className="btn btn-danger" disabled={bulkBusy} onClick={deleteSelected}>{bulkBusy ? 'Deleting…' : 'Delete'}</button>
              </span>
            ) : (
              <button type="button" className="btn btn-q" style={{ color: '#ff8a9a' }} onClick={() => setConfirmBulk(true)}>Delete {selected.length}</button>
            )}
          </div>
        )}
        {bulkError && <Alert title="Nothing was deleted">{bulkError}</Alert>}
        <div className="card table-box">
          {data.hooks.length === 0 ? (
            <p className="empty">No hooks yet. Add the hooks you run, or <Link to={`/projects/${code}/import`}>import a Meta file</Link> and pick hooks for its ad names.</p>
          ) : rows.length === 0 ? (
            <p className="empty">Nothing matches this filter.</p>
          ) : (
            <table style={{ minWidth: 900 }}>
              <thead>
                <tr>
                  <th style={{ width: 44 }}><input type="checkbox" aria-label="Select all hooks shown" checked={allChecked} onChange={toggleAll} /></th>
                  <th>#</th><th>HOOK</th><th>TIER</th><th className="num">IMPRESSIONS</th><th className="num">HOOK RATE</th><th className="num">PURCHASES</th><th className="num">CPA</th>
                </tr>
              </thead>
              <tbody>
                {rows.map((h) => editing === h.id ? (
                  <tr key={h.id}><td colSpan={8}><EditHook hook={h} project={data} onDone={(saved) => { setEditing(null); if (saved) { reload(); setChecked(new Set()) } }} /></td></tr>
                ) : (
                  <tr key={h.id}>
                    <td><input type="checkbox" aria-label={`Select ${h.number}`} checked={checked.has(h.id)} onChange={() => toggle(h.id)} /></td>
                    <td className="strong muted">{h.number}</td>
                    <td style={{ maxWidth: 420 }}>
                      {h.text ? (
                        <button type="button" className="link-btn" style={{ color: 'var(--text)', fontWeight: 600, textAlign: 'left' }} onClick={() => setEditing(h.id)}>{h.text}</button>
                      ) : (
                        <button type="button" className="link-btn" onClick={() => setEditing(h.id)}>+ Add text</button>
                      )}
                      <div className="hint" style={{ marginTop: 2 }}><span className="mono" style={{ fontSize: 12 }}>{h.adName}</span>{h.type && ` · ${h.type}`}</div>
                    </td>
                    <td><span className={'chip tier-' + h.tier} title={h.reason}>{h.tier === 'pending' ? 'No results' : tierLabel[h.tier]}</span></td>
                    <td className="num">{h.tier === 'pending' ? '—' : int(h.impressions)}</td>
                    <td className="num">{h.tier === 'pending' ? '—' : pct(h.hookRate)}</td>
                    <td className="num">{h.tier === 'pending' ? '—' : int(h.purchases)}</td>
                    <td className="num strong">{money(h.cpaCents)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </div>
      </section>

      <div className="split" style={{ gap: 12 }}>
        <section aria-labelledby="desc-title" className="card pad grow" style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
          <div className="header">
            <h2 id="desc-title" className="section">DESCRIPTION</h2>
            <Link to={`/projects/${code}/settings`} className="hint">Edit</Link>
          </div>
          <p className="muted" style={{ margin: 0, whiteSpace: 'pre-line' }}>{data.description || 'No description yet. The generator works better with one.'}</p>
        </section>
        <section aria-labelledby="types-title" className="card pad side-col" style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
          <h2 id="types-title" className="section">CPA BY TYPE</h2>
          {data.types.length === 0 && <p className="muted" style={{ margin: 0 }}>Appears once hooks have text and results.</p>}
          {data.types.map((t) => (
            <div key={t.type} style={{ display: 'grid', gridTemplateColumns: '110px minmax(0, 1fr) 60px', alignItems: 'center', gap: 10 }}>
              <span style={{ fontSize: 13, fontWeight: 600 }}>{t.type}</span>
              <span style={{ height: 8, borderRadius: 999, background: 'rgba(255,255,255,0.06)' }}>
                <span style={{ display: 'block', height: 8, borderRadius: 999, width: `${(t.cpaCents / maxTypeCpa) * 100}%`, background: t.cpaCents <= data.settings.maxCpaCents ? '#a78bfa' : '#fb923c' }} />
              </span>
              <span className="num strong" style={{ fontSize: 13 }}>{money(t.cpaCents)}</span>
            </div>
          ))}
        </section>
      </div>
    </div>
  )
}

function AddHook({ code, pattern, onAdded }: { code: string; pattern: string; onAdded: () => void }) {
  const { refreshNav } = useLayout()
  const [open, setOpen] = useState(false)
  const [text, setText] = useState('')
  const [screen, setScreen] = useState('')
  const [fieldError, setFieldError] = useState('')
  const [error, setError] = useState('')
  const [added, setAdded] = useState('')
  const [busy, setBusy] = useState(false)

  async function submit(e: FormEvent) {
    e.preventDefault()
    setError('')
    if (!text.trim()) {
      setFieldError('Write the spoken first line of the ad.')
      return
    }
    setBusy(true)
    try {
      const { adName } = await api.addHook(code, { text, onScreenText: screen })
      setAdded(adName)
      setText('')
      setScreen('')
      onAdded()
      refreshNav()
    } catch (err) {
      setError(errorText(err))
    } finally {
      setBusy(false)
    }
  }

  if (!open) {
    return (
      <div className="row">
        <button type="button" className="btn btn-g" onClick={() => setOpen(true)}>+ Add hook</button>
        <span className="hint">Hooks you already run. Name the ad in Meta like <span className="mono">{pattern}</span></span>
      </div>
    )
  }
  return (
    <form className="card pad" onSubmit={submit} noValidate style={{ display: 'flex', flexDirection: 'column', gap: 14 }} aria-label="Add hook">
      {added && <Alert kind="success" title="Hook added">Name the ad in Meta Ads Manager <span className="mono">{added}</span> so results match it.</Alert>}
      {error && <Alert title="The hook was not added">{error}</Alert>}
      <div className="grid-fields" style={{ gridTemplateColumns: 'minmax(0, 2fr) minmax(0, 1fr)' }}>
        <div className="field">
          <label htmlFor="new-hook" className="lbl">HOOK</label>
          <input id="new-hook" className="input" maxLength={300} value={text} autoFocus aria-invalid={!!fieldError} aria-describedby="new-hook-error"
            onChange={(e) => { setText(e.target.value); setFieldError('') }} />
          <FieldError id="new-hook-error">{fieldError}</FieldError>
        </div>
        <div className="field">
          <label htmlFor="new-screen" className="lbl">ON SCREEN</label>
          <input id="new-screen" className="input" maxLength={120} value={screen} onChange={(e) => setScreen(e.target.value)} />
        </div>
      </div>
      <div className="row" style={{ justifyContent: 'flex-end' }}>
        <button type="button" className="btn btn-q" onClick={() => { setOpen(false); setAdded(''); setError('') }}>Close</button>
        <button type="submit" className="btn btn-p" disabled={busy}>{busy ? 'Adding…' : 'Add hook'}</button>
      </div>
    </form>
  )
}

function EditHook({ hook, project, onDone }: { hook: HookRow; project: ProjectView; onDone: (saved: boolean) => void }) {
  const standard = `${project.market}_${project.code}_${hook.number}`
  const [text, setText] = useState(hook.text)
  const [screen, setScreen] = useState(hook.onScreenText)
  const [adName, setAdName] = useState(hook.adName)
  const [fieldError, setFieldError] = useState('')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const [confirmDelete, setConfirmDelete] = useState(false)

  async function remove() {
    setError('')
    setBusy(true)
    try {
      await api.deleteHook(hook.id)
      onDone(true)
    } catch (err) {
      setError(errorText(err))
      setBusy(false)
    }
  }

  async function save(e: FormEvent) {
    e.preventDefault()
    setError('')
    if (!text.trim()) {
      setFieldError('Write the spoken first line of the ad.')
      return
    }
    setBusy(true)
    try {
      await api.updateHook(hook.id, { text, onScreenText: screen, adName })
      onDone(true)
    } catch (err) {
      setError(errorText(err))
      setBusy(false)
    }
  }

  return (
    <form onSubmit={save} noValidate style={{ display: 'flex', flexDirection: 'column', gap: 14, padding: '6px 0' }} aria-label={`Edit ${hook.number}`}>
      <span className="lbl" style={{ color: '#c4b5fd' }}>EDIT {hook.number}</span>
      {error && <Alert title="Not saved">{error}</Alert>}
      <div className="grid-fields" style={{ gridTemplateColumns: 'minmax(0, 2fr) minmax(0, 1fr)' }}>
        <div className="field">
          <label htmlFor={`text-${hook.id}`} className="lbl">HOOK</label>
          <input id={`text-${hook.id}`} className="input" maxLength={300} value={text} autoFocus aria-invalid={!!fieldError} aria-describedby={`text-${hook.id}-error`}
            onChange={(e) => { setText(e.target.value); setFieldError('') }} />
          <FieldError id={`text-${hook.id}-error`}>{fieldError}</FieldError>
        </div>
        <div className="field">
          <label htmlFor={`screen-${hook.id}`} className="lbl">ON SCREEN</label>
          <input id={`screen-${hook.id}`} className="input" maxLength={120} value={screen} placeholder="Text on the video, up to 8 words" onChange={(e) => setScreen(e.target.value)} />
        </div>
      </div>
      <div className="field">
        <label htmlFor={`ad-${hook.id}`} className="lbl">AD NAME IN META</label>
        <input id={`ad-${hook.id}`} className="input mono" maxLength={200} value={adName} onChange={(e) => setAdName(e.target.value)} />
        <span className="hint">Change it if the ad is named differently in Ads Manager. Standard name: <button type="button" className="link-btn mono" onClick={() => setAdName(standard)}>{standard}</button></span>
      </div>
      <div className="row" style={{ justifyContent: 'space-between' }}>
        {confirmDelete ? (
          <div className="row" style={{ gap: 8 }}>
            <span className="hint">Delete {hook.number} with its results? Its number is not used again.</span>
            <button type="button" className="btn btn-q" onClick={() => setConfirmDelete(false)}>Keep</button>
            <button type="button" className="btn btn-danger" disabled={busy} onClick={remove}>Delete</button>
          </div>
        ) : (
          <button type="button" className="btn btn-q" style={{ color: '#ff8a9a' }} onClick={() => setConfirmDelete(true)}>Delete hook</button>
        )}
        <div className="row" style={{ gap: 8 }}>
          <button type="button" className="btn btn-q" onClick={() => onDone(false)}>Cancel</button>
          <button type="submit" className="btn btn-p" disabled={busy}>{busy ? 'Saving…' : 'Save'}</button>
        </div>
      </div>
    </form>
  )
}

import { useState, type ChangeEvent, type DragEvent } from 'react'
import { Link, useParams } from 'react-router-dom'
import { api, type HookRow, type ImportView } from '../api'
import { money, plural } from '../format'
import { useLayout } from '../Layout'
import { ProjectTabs } from '../ProjectTabs'
import { Alert, Help } from '../ui'
import { errorText, useLoad } from '../useLoad'

export function ImportPage() {
  const { code = '' } = useParams()
  const { refreshNav } = useLayout()
  const project = useLoad(() => api.project(code), [code])
  const latest = useLoad(() => api.latestImport(code), [code])
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const [over, setOver] = useState(false)

  function onPick(e: ChangeEvent<HTMLInputElement>) {
    const file = e.target.files?.[0]
    e.target.value = ''
    if (file) upload(file)
  }

  function onDrop(e: DragEvent<HTMLLabelElement>) {
    e.preventDefault()
    setOver(false)
    if (busy) return
    const files = e.dataTransfer.files
    if (files.length !== 1) {
      setError('Drop one file at a time.')
      return
    }
    if (!files[0].name.toLowerCase().endsWith('.csv')) {
      setError(`${files[0].name} is not a CSV file. Export the table from Ads Manager as CSV.`)
      return
    }
    upload(files[0])
  }

  async function upload(file: File) {
    setBusy(true)
    setError('')
    try {
      latest.setData(await api.importMeta(code, file))
      project.reload()
      refreshNav()
    } catch (err) {
      setError(errorText(err))
    } finally {
      setBusy(false)
    }
  }

  if (project.error) return <Alert title="Could not load the project">{project.error.message}</Alert>
  if (!project.data) return null
  const p = project.data
  const imp = latest.data
  const missing = p.hooks.filter((h) => h.text === '').length

  return (
    <div className="page">
      <header className="header">
        <h1>{p.code} · <span className="grad">IMPORT</span></h1>
      </header>
      <ProjectTabs code={p.code} open={p.openCandidates} />

      <label className={'dropzone' + (over ? ' over' : '') + (busy ? ' busy' : '')}
        onDragOver={(e) => { e.preventDefault(); setOver(true) }} onDragLeave={() => setOver(false)} onDrop={onDrop}>
        <input type="file" accept=".csv,text/csv" disabled={busy} onChange={onPick} aria-label="Meta Ads export, CSV file" />
        <svg width="36" height="36" viewBox="0 0 24 24" aria-hidden="true" fill="none" stroke="#A78BFA" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round">
          <path d="M12 15V4" /><path d="m7 9 5-5 5 5" /><path d="M4 15v4a1 1 0 0 0 1 1h14a1 1 0 0 0 1-1v-4" />
        </svg>
        <span className="title">{busy ? 'Uploading…' : 'Drop the Meta Ads CSV here'}</span>
        <span className="hint">or click to choose a file</span>
      </label>
      <p className="hint" style={{ margin: '-8px 4px 0' }}>
        Export from the <strong>Ads</strong> tab of Ads Manager, not Campaigns or Ad sets, with English column names.
        Otherwise the file has no <span className="mono">Ad name</span> column and rows cannot find their hooks.
      </p>

      {error && <Alert title="The file was not imported">{error}</Alert>}

      <div className="card pad" style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
        <span className="lbl">HOW ROWS FIND THEIR HOOK</span>
        <span className="muted">
          Rows whose ad name is <span className="mono">{p.adNamePattern.replace('H01', 'H<number>')}</span> go to that hook.
          A number the project does not have yet and any other ad name are listed below the result:
          pick its hook once, and later files match it by themselves. No hook is created without you.
        </span>
      </div>

      {!imp && latest.error && latest.error.status !== 404 && <Alert title="Could not load the last import">{latest.error.message}</Alert>}

      {imp && (
        <>
          <div className="card row" style={{ gap: 14, padding: '16px 20px' }}>
            <svg width="28" height="28" viewBox="0 0 28 28" aria-hidden="true"><path d="M7 3h10l5 5v17H7z" fill="none" stroke="#A78BFA" strokeWidth="1.6" strokeLinejoin="round" /><path d="M17 3v5h5" fill="none" stroke="#A78BFA" strokeWidth="1.6" strokeLinejoin="round" /></svg>
            <span style={{ fontWeight: 700 }}>{imp.fileName}</span>
            <span className="muted">{imp.periodStart} – {imp.periodEnd}</span>
          </div>

          <section aria-label="Result" className="kpis">
            <div className="card kpi"><span className="k">Rows used</span><span className="v" style={{ color: '#ddd6fe' }}>{imp.rowsMatched} <span className="muted" style={{ fontSize: 15 }}>/ {imp.rowsTotal}</span></span></div>
            <div className="card kpi"><span className="k">Hooks updated</span><span className="v">{imp.hooksUpdated}</span></div>
            <div className="card kpi"><span className="k">New hooks</span><span className="v">{imp.hooksCreated}</span></div>
          </section>

          {missing > 0 && (
            <Alert kind="info" title={`${plural(missing, 'hook needs', 'hooks need')} text`}
              action={<Link to={`/projects/${code}?filter=notext`} className="btn btn-g" style={{ minHeight: 36 }}>Fill in</Link>}>
              The file knows their results, not their words. Add the text to use them for generation.
            </Alert>
          )}

          {imp.skippedOtherMarkets > 0 && (
            <p className="hint" style={{ margin: 0 }}>Ignored: {plural(imp.skippedOtherMarkets, 'row', 'rows')} of other markets.</p>
          )}

          {imp.unrecognized.length > 0 && (
            <AssignNames code={code} hooks={p.hooks} names={imp.unrecognized}
              onAssigned={(v) => { latest.setData(v); project.reload(); refreshNav() }} />
          )}
        </>
      )}
    </div>
  )
}

function AssignNames({ code, hooks, names, onAssigned }: {
  code: string; hooks: HookRow[]; names: ImportView['unrecognized']; onAssigned: (v: ImportView) => void
}) {
  const [choice, setChoice] = useState<Record<string, string>>({})
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [done, setDone] = useState('')
  const sorted = [...names].sort((a, b) => b.spendCents - a.spendCents)
  // Names of this project and names without a convention come first; names of
  // other project codes are usually other products in the same ad account.
  const mine = sorted.filter((u) => !u.project || u.project === code)
  const others = sorted.filter((u) => u.project && u.project !== code)
  const otherCodes = [...new Set(others.map((u) => u.project))]
  const withText = hooks.filter((h) => h.text !== '').sort((a, b) => hookNumber(a.number) - hookNumber(b.number))
  const picked = Object.entries(choice).filter(([, v]) => v !== '')

  async function apply() {
    setBusy(true)
    setError('')
    setDone('')
    try {
      const v = await api.assignAdNames(code, picked.map(([adName, v]) => ({ adName, hookId: v === 'new' ? 0 : Number(v) })))
      setChoice({})
      setDone(`${plural(picked.length, 'ad name', 'ad names')} assigned. Their results are in the hooks now.`)
      onAssigned(v)
    } catch (err) {
      setError(errorText(err))
    } finally {
      setBusy(false)
    }
  }

  function row(u: ImportView['unrecognized'][number]) {
    return (
      <div key={u.adName} className="header" style={{ padding: '12px 20px', borderBottom: '1px solid var(--line-soft)', gap: 12 }}>
        <span className="row" style={{ gap: 8, minWidth: 0 }}>
          <span className="mono" style={{ overflowWrap: 'anywhere' }}>{u.adName}</span>
          {u.project === code && <span className="chip" title="Follows this project's naming, but the project has no hook with this number">no hook yet</span>}
          {u.project && u.project !== code && <span className="chip" title="Follows the naming of another project code">{u.project}</span>}
        </span>
        <span className="row" style={{ gap: 12, flexWrap: 'nowrap' }}>
          <span className="muted" style={{ whiteSpace: 'nowrap' }}>{money(u.spendCents, 0)}</span>
          <select className="input" style={{ width: 220, minHeight: 38, padding: '4px 10px' }} aria-label={`Hook for ${u.adName}`}
            value={choice[u.adName] ?? ''} onChange={(e) => setChoice({ ...choice, [u.adName]: e.target.value })}>
            <option value="">Skip</option>
            <option value="new">New hook</option>
            {withText.map((h) => <option key={h.id} value={h.id}>{h.number} · {h.text.slice(0, 40)}</option>)}
          </select>
        </span>
      </div>
    )
  }

  return (
    <section aria-labelledby="unknown-title" className="card" style={{ overflow: 'hidden' }}>
      <div className="header" style={{ padding: '18px 20px', borderBottom: '1px solid var(--line-soft)' }}>
        <span className="lbl-row">
          <h2 id="unknown-title" className="section">AD NAMES WITHOUT A HOOK · {mine.length}</h2>
          <Help>
            Rows of the file that no hook of this project matched. Pick a hook with text for the ones that belong here, or New hook.
            New hook keeps the number from the name when it is free. Rows you leave on Skip stay out.
          </Help>
        </span>
        <div className="row" style={{ gap: 8 }}>
          {mine.length > 0 && (
            <button type="button" className="pill sm" onClick={() => setChoice({ ...choice, ...Object.fromEntries(mine.map((n) => [n.adName, 'new'])) })}>All → new hooks</button>
          )}
          {picked.length > 0 && <button type="button" className="link-btn" onClick={() => setChoice({})}>Clear</button>}
        </div>
      </div>
      {error && <div style={{ padding: '14px 20px 0' }}><Alert title="Nothing was assigned">{error}</Alert></div>}
      {done && <div style={{ padding: '14px 20px 0' }}><Alert kind="success" title="Done">{done}</Alert></div>}
      {mine.length === 0 && <p className="muted" style={{ margin: 0, padding: '14px 20px', borderBottom: '1px solid var(--line-soft)' }}>Every row of this project found its hook.</p>}
      {mine.map(row)}
      {others.length > 0 && (
        <details style={{ borderBottom: '1px solid var(--line-soft)' }}>
          <summary style={{ cursor: 'pointer', padding: '14px 20px' }}>
            <span className="lbl">OTHER PROJECTS IN THIS FILE · {others.length}</span>{' '}
            <span className="hint">· {otherCodes.join(', ')} · skipped unless you pick a hook</span>
          </summary>
          {others.map(row)}
        </details>
      )}
      <div className="row" style={{ justifyContent: 'flex-end', padding: '14px 20px' }}>
        <button type="button" className="btn btn-p" disabled={busy || picked.length === 0} onClick={apply}>
          {busy ? 'Applying…' : picked.length === 0 ? 'Pick hooks to apply' : `Apply ${picked.length}`}
        </button>
      </div>
    </section>
  )
}

// hookNumber turns "H07" into 7 for sorting.
function hookNumber(n: string) {
  return Number(n.slice(1)) || 0
}

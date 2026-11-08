import { useEffect, useState } from 'react'
import { api, type QueueStatus } from '../api'
import { useLayout } from '../Layout'
import { Alert } from '../ui'
import { errorText, useLoad } from '../useLoad'

const tabs: { key: QueueStatus; label: string; last: string }[] = [
  { key: 'ready', label: 'Ready', last: 'SOURCE' },
  { key: 'waiting', label: 'Waiting for results', last: 'EXPORTED' },
  { key: 'live', label: 'Live', last: '' },
]

export function QueuePage() {
  const { refreshNav } = useLayout()
  const [tab, setTab] = useState<QueueStatus>('ready')
  const { data, error, reload } = useLoad(() => api.queue(tab), [tab])
  const [off, setOff] = useState<Set<number>>(new Set())
  const [busy, setBusy] = useState(false)
  const [notice, setNotice] = useState('')
  const [actionError, setActionError] = useState('')

  useEffect(() => setOff(new Set()), [tab])

  const items = data?.items ?? []
  const checked = tab === 'ready' ? items.filter((i) => !off.has(i.id)) : []
  const toggle = (id: number) => setOff((s) => { const n = new Set(s); if (n.has(id)) n.delete(id); else n.add(id); return n })

  async function download() {
    setBusy(true)
    setActionError('')
    try {
      const blob = await api.exportCsv(checked.map((i) => i.id))
      const url = URL.createObjectURL(blob)
      const a = document.createElement('a')
      a.href = url
      a.download = 'hooks_for_production.csv'
      a.click()
      URL.revokeObjectURL(url)
      setNotice(`${checked.length} hooks moved to Waiting`)
      reload()
      refreshNav()
    } catch (e) {
      setActionError(errorText(e))
    } finally {
      setBusy(false)
    }
  }

  async function copyNames() {
    try {
      await navigator.clipboard.writeText(checked.map((i) => i.adName).join('\n'))
      setNotice('Ad names copied')
    } catch {
      setActionError('Clipboard is not available')
    }
  }

  return (
    <div className="page">
      <header className="header">
        <h1>PRODUCTION <span className="grad">QUEUE</span></h1>
        {tab === 'ready' && (
          <div className="row">
            <button type="button" className="btn btn-g" disabled={!checked.length} onClick={copyNames}>Copy ad names</button>
            <button type="button" className="btn btn-p" disabled={busy || !checked.length} onClick={download}>Download CSV · {checked.length}</button>
          </div>
        )}
      </header>

      <div role="tablist" aria-label="Status" className="row" style={{ gap: 8 }}>
        {tabs.map((t) => (
          <button key={t.key} type="button" role="tab" aria-selected={tab === t.key} className={'tab' + (tab === t.key ? ' on' : '')} onClick={() => { setTab(t.key); setNotice('') }}>
            {t.label} <span className="cnt inline">{data?.counts[t.key] ?? 0}</span>
          </button>
        ))}
      </div>

      {notice && <Alert kind="success" title={notice} />}
      {(error || actionError) && <Alert title="That did not work">{actionError || error?.message}</Alert>}

      <div className="card table-box">
        {items.length === 0 ? (
          <p className="empty">{tab === 'ready' ? 'Nothing to export. Select hooks in a project’s Candidates tab.' : 'Nothing here yet.'}</p>
        ) : (
          <table style={{ minWidth: 820 }}>
            <thead>
              <tr>
                <th style={{ width: 34 }}>
                  {tab === 'ready' && <input type="checkbox" aria-label="Select all" checked={off.size === 0} onChange={() => setOff(off.size === 0 ? new Set(items.map((i) => i.id)) : new Set())} />}
                </th>
                <th>AD NAME</th><th>HOOK</th><th>TYPE</th>
                <th style={{ textAlign: 'right' }}>{tabs.find((t) => t.key === tab)?.last}</th>
              </tr>
            </thead>
            <tbody>
              {items.map((i) => (
                <tr key={i.id}>
                  <td>{tab === 'ready' && <input type="checkbox" aria-label={`Include ${i.adName}`} checked={!off.has(i.id)} onChange={() => toggle(i.id)} />}</td>
                  <td><code style={{ color: '#e9d5ff', whiteSpace: 'nowrap' }}>{i.adName}</code></td>
                  <td style={{ fontWeight: 600, maxWidth: 380 }}>{i.text}</td>
                  <td className="muted" style={{ whiteSpace: 'nowrap' }}>{i.type || '—'}</td>
                  <td className="muted" style={{ textAlign: 'right', whiteSpace: 'nowrap', fontWeight: 700 }}>{i.note}</td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>
    </div>
  )
}

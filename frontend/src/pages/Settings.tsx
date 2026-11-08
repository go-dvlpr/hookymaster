import { useEffect, useState, type FormEvent } from 'react'
import { useNavigate, useParams } from 'react-router-dom'
import { api, codePattern, type Settings } from '../api'
import { useLayout } from '../Layout'
import { ProjectTabs } from '../ProjectTabs'
import { Alert, FieldError, Help } from '../ui'
import { errorText, useLoad } from '../useLoad'

type Form = Record<keyof Settings, string>

const toForm = (s: Settings): Form => ({
  code: s.code, name: s.name, description: s.description, buyer: s.buyer,
  minImpressions: String(s.minImpressions), minPurchases: String(s.minPurchases),
  maxCpaCents: (s.maxCpaCents / 100).toFixed(2), refSharePct: String(s.refSharePct),
  minRefs: String(s.minRefs), maxRefs: String(s.maxRefs), maxAnti: String(s.maxAnti),
  weightCpa: String(s.weightCpa), weightHookRate: String(s.weightHookRate),
  weightPurchases: String(s.weightPurchases), weightCtr: String(s.weightCtr),
})

type NumberField = { key: keyof Settings; label: string; help: string; prefix?: string; suffix?: string }

const numbers: NumberField[] = [
  { key: 'minImpressions', label: 'MIN IMPRESSIONS', help: 'A hook with fewer impressions gets "No data": too little traffic to judge. It is not ranked and is not used for generation.' },
  { key: 'minPurchases', label: 'MIN PURCHASES', help: 'A hook with fewer purchases is "Early": its CPA can still change a lot. It shows its numbers but is not used for generation.' },
  { key: 'maxCpaCents', label: 'MAX CPA', prefix: '$', help: 'The CPA you are ready to pay. Hooks above it, with enough purchases to be sure, become anti-examples. Hooks under it can become references.' },
  { key: 'refSharePct', label: 'REFERENCES SHARE', suffix: '%', help: 'Which part of the hooks under max CPA, best score first, become references for the next batch. Example: 20 such hooks × 15% = 3 references. At most 2 references share one hook type.' },
  { key: 'minRefs', label: 'MIN REFERENCES', help: 'Take at least this many best hooks, even when the share gives fewer. Only hooks under max CPA count.' },
  { key: 'maxRefs', label: 'MAX REFERENCES', help: 'Never more references than this, even when the share gives more. Keeps the batch focused on the very best.' },
  { key: 'maxAnti', label: 'MAX ANTI-EXAMPLES', help: 'How many hooks with the worst CPA the generator sees as examples of what to avoid.' },
]

const weights: NumberField[] = [
  { key: 'weightCpa', label: 'CPA', suffix: '%', help: 'How much a low cost per purchase counts. The main business metric.' },
  { key: 'weightHookRate', label: 'HOOK RATE', suffix: '%', help: '3-second plays per impression: how well the opening stops the scroll.' },
  { key: 'weightPurchases', label: 'PURCHASES', suffix: '%', help: 'How many purchases the hook brought. More purchases also mean a more reliable CPA.' },
  { key: 'weightCtr', label: 'CTR', suffix: '%', help: 'Link clicks per impression: how many viewers want to know more.' },
]

const weightSum = (f: Form) => weights.reduce((sum, w) => sum + (Number(f[w.key]) || 0), 0)

function validate(f: Form): Partial<Record<keyof Settings, string>> {
  const e: Partial<Record<keyof Settings, string>> = {}
  if (!codePattern.test(f.code.trim().toUpperCase())) e.code = 'Use 1 to 12 letters or digits, starting with a letter, e.g. O12.'
  if (!f.name.trim()) e.name = 'Give the project a name.'
  for (const n of [...numbers, ...weights]) {
    const v = Number(f[n.key])
    if (f[n.key].trim() === '' || Number.isNaN(v) || v < 0) e[n.key] = 'Enter a number, 0 or more.'
  }
  if (!e.refSharePct && (Number(f.refSharePct) < 1 || Number(f.refSharePct) > 100)) e.refSharePct = 'Use 1 to 100%.'
  if (!e.maxCpaCents && Number(f.maxCpaCents) <= 0) e.maxCpaCents = 'Must be above $0.'
  if (!e.maxRefs && Number(f.maxRefs) < 1) e.maxRefs = 'At least 1.'
  if (!e.minRefs && !e.maxRefs && Number(f.minRefs) > Number(f.maxRefs)) e.minRefs = 'Must not be above max references.'
  if (!weights.some((w) => e[w.key]) && weightSum(f) !== 100) e.weightCpa = `Weights add up to ${weightSum(f)}%, make them 100%.`
  return e
}

export function SettingsPage() {
  const { code = '' } = useParams()
  const navigate = useNavigate()
  const { refreshNav } = useLayout()
  const [confirmDelete, setConfirmDelete] = useState(false)
  const [deleteError, setDeleteError] = useState('')
  const { data, error } = useLoad(() => api.project(code), [code])
  const [form, setForm] = useState<Form | null>(null)
  const [errors, setErrors] = useState<Partial<Record<keyof Settings, string>>>({})
  const [saveError, setSaveError] = useState('')
  const [saved, setSaved] = useState(false)
  const [busy, setBusy] = useState(false)

  useEffect(() => { if (data) setForm(toForm(data.settings)) }, [data])

  if (error) return <Alert title="Could not load the project">{error.message}</Alert>
  if (!data || !form) return null

  const set = (k: keyof Settings) => (e: { target: { value: string } }) => {
    setForm({ ...form, [k]: e.target.value })
    setErrors({ ...errors, [k]: undefined })
    setSaved(false)
  }

  function numberField(f: NumberField) {
    return (
      <div key={f.key} className="field">
        <span className="lbl-row">
          <label htmlFor={f.key} className="lbl">{f.label}</label>
          <Help>{f.help}</Help>
        </span>
        <div className="row" style={{ flexWrap: 'nowrap', gap: 6 }}>
          {f.prefix && <span className="muted">{f.prefix}</span>}
          <input id={f.key} className="input" type="number" min={0} step={f.key === 'maxCpaCents' ? '0.01' : '1'} inputMode="decimal"
            value={form![f.key]} aria-invalid={!!errors[f.key]} aria-describedby={`${f.key}-error`} onChange={set(f.key)} />
          {f.suffix && <span className="muted">{f.suffix}</span>}
        </div>
        <FieldError id={`${f.key}-error`}>{errors[f.key]}</FieldError>
      </div>
    )
  }

  async function removeProject() {
    setDeleteError('')
    try {
      await api.deleteProject(code)
      refreshNav()
      navigate('/', { replace: true })
    } catch (err) {
      setDeleteError(errorText(err))
    }
  }

  async function save(e: FormEvent) {
    e.preventDefault()
    setSaveError('')
    const found = validate(form!)
    setErrors(found)
    if (Object.keys(found).length > 0) return
    setBusy(true)
    try {
      const s = await api.saveSettings(code, {
        code: form!.code.trim().toUpperCase(), name: form!.name, description: form!.description, buyer: form!.buyer,
        minImpressions: Number(form!.minImpressions), minPurchases: Number(form!.minPurchases),
        maxCpaCents: Math.round(Number(form!.maxCpaCents) * 100), refSharePct: Number(form!.refSharePct),
        minRefs: Number(form!.minRefs), maxRefs: Number(form!.maxRefs), maxAnti: Number(form!.maxAnti),
        weightCpa: Number(form!.weightCpa), weightHookRate: Number(form!.weightHookRate),
        weightPurchases: Number(form!.weightPurchases), weightCtr: Number(form!.weightCtr),
      })
      setForm(toForm(s))
      setSaved(true)
      refreshNav()
      if (s.code !== code) navigate(`/projects/${s.code}/settings`, { replace: true })
    } catch (err) {
      setSaveError(errorText(err))
    } finally {
      setBusy(false)
    }
  }

  return (
    <form className="page" onSubmit={save} noValidate aria-label="Project settings">
      <header style={{ display: 'flex', flexDirection: 'column', gap: 18 }}>
        <h1>{data.code} · <span className="grad">SETTINGS</span></h1>
        <ProjectTabs code={data.code} open={data.openCandidates} />
      </header>

      {saved && <Alert kind="success" title="Settings saved">Ranking and the next batch use them now.</Alert>}
      {saveError && <Alert title="Settings were not saved">{saveError}</Alert>}

      <section className="card pad" style={{ display: 'flex', flexDirection: 'column', gap: 18 }}>
        <h2 className="section">PROJECT</h2>
        <div className="field">
          <span className="lbl-row">
            <label htmlFor="code" className="lbl">CODE</label>
            <Help>The middle part of ad names: {data.code} in {data.adNamePattern}. Set the code your ads in Meta already use. Changing it changes the ad names of all hooks, so ads already running under the old names stop matching.</Help>
          </span>
          <input id="code" className="input mono" maxLength={12} value={form.code} style={{ maxWidth: 200, textTransform: 'uppercase' }}
            aria-invalid={!!errors.code} aria-describedby="code-error" onChange={set('code')} />
          <FieldError id="code-error">{errors.code}</FieldError>
        </div>
        <div className="field">
          <label htmlFor="name" className="lbl">NAME</label>
          <input id="name" className="input" maxLength={80} value={form.name} aria-invalid={!!errors.name} aria-describedby="name-error" onChange={set('name')} />
          <FieldError id="name-error">{errors.name}</FieldError>
        </div>
        <div className="field">
          <span className="lbl-row">
            <label htmlFor="description" className="lbl">DESCRIPTION</label>
            <Help>What you sell, to whom and the idea of this ad concept. The generator reads it for every batch, so be specific.</Help>
          </span>
          <textarea id="description" className="input" rows={4} maxLength={3000} value={form.description} onChange={set('description')} />
        </div>
        <div className="field">
          <span className="lbl-row">
            <label htmlFor="buyer" className="lbl">BUYER</label>
            <Help>Who really buys, if it is not the person in the video: their age, situation and what bothers them. Example: women 40–60 who always run hot, or adult kids buying for a parent. The generator writes the hooks for this person.</Help>
          </span>
          <textarea id="buyer" className="input" rows={3} maxLength={1000} value={form.buyer}
            placeholder="Who really buys and why. Often not the person in the video." onChange={set('buyer')} />
        </div>
      </section>

      <section className="card pad" style={{ display: 'flex', flexDirection: 'column', gap: 18 }}>
        <div style={{ display: 'flex', flexDirection: 'column', gap: 4 }}>
          <h2 className="section">RANKING AND REFERENCES</h2>
          <span className="hint">Which hooks the generator builds on, and which it avoids. Hover <b>?</b> for details.</span>
        </div>
        <div className="grid-fields" style={{ gridTemplateColumns: 'repeat(auto-fill, minmax(200px, 1fr))' }}>
          {numbers.map(numberField)}
        </div>
      </section>

      <section className="card pad" style={{ display: 'flex', flexDirection: 'column', gap: 18 }}>
        <div style={{ display: 'flex', flexDirection: 'column', gap: 4 }}>
          <span className="lbl-row">
            <h2 className="section">REFERENCE SCORE</h2>
            <Help>Hooks with enough data get a score from their place among the project's hooks on each metric. The best scores under max CPA become references. Weights add up to 100%.</Help>
          </span>
          <span className={'hint' + (weightSum(form) !== 100 ? ' err' : '')}>Total {weightSum(form)}% of 100%</span>
        </div>
        <div className="grid-fields" style={{ gridTemplateColumns: 'repeat(auto-fill, minmax(110px, 1fr))' }}>
          {weights.map(numberField)}
        </div>
      </section>

      <div className="row" style={{ justifyContent: 'flex-end' }}>
        <button type="submit" className="btn btn-p" disabled={busy}>{busy ? 'Saving…' : 'Save settings'}</button>
      </div>

      <section className="card pad danger-zone" aria-labelledby="delete-title">
        <div style={{ display: 'flex', flexDirection: 'column', gap: 4 }}>
          <h2 id="delete-title" className="section">DELETE PROJECT</h2>
          <span className="hint">Removes the project with all its hooks, candidates, imports and results. This cannot be undone.</span>
        </div>
        {deleteError && <Alert title="The project was not deleted">{deleteError}</Alert>}
        {confirmDelete ? (
          <div className="row" style={{ gap: 8 }}>
            <span className="strong">Delete {data.code} · {data.name} for good?</span>
            <button type="button" className="btn btn-g" onClick={() => setConfirmDelete(false)}>Cancel</button>
            <button type="button" className="btn btn-danger" onClick={removeProject}>Delete project</button>
          </div>
        ) : (
          <div><button type="button" className="btn btn-g" onClick={() => setConfirmDelete(true)}>Delete project…</button></div>
        )}
      </section>
    </form>
  )
}

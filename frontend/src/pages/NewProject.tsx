import { useState, type FormEvent } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { api, codePattern } from '../api'
import { useLayout } from '../Layout'
import { Alert, FieldError, Help } from '../ui'
import { errorText } from '../useLoad'

export function NewProjectPage() {
  const navigate = useNavigate()
  const { refreshNav } = useLayout()
  const [code, setCode] = useState('')
  const [codeError, setCodeError] = useState('')
  const [name, setName] = useState('')
  const [description, setDescription] = useState('')
  const [buyer, setBuyer] = useState('')
  const [nameError, setNameError] = useState('')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)

  async function submit(e: FormEvent) {
    e.preventDefault()
    setError('')
    const c = code.trim().toUpperCase()
    const badCode = c !== '' && !codePattern.test(c)
    if (badCode) setCodeError('Use 1 to 12 letters or digits, starting with a letter, e.g. O12.')
    if (!name.trim()) setNameError('Give the project a name, for example “Gym bag”.')
    if (badCode || !name.trim()) return
    setBusy(true)
    try {
      const created = await api.createProject({ code: c, name, description, buyer })
      refreshNav()
      navigate(`/projects/${created.code}`)
    } catch (err) {
      setError(errorText(err))
      setBusy(false)
    }
  }

  return (
    <form className="page narrow" onSubmit={submit} noValidate aria-label="New project">
      <h1>NEW <span className="grad">PROJECT</span></h1>

      {error && <Alert title="The project was not created">{error}</Alert>}

      <section className="card pad" style={{ display: 'flex', flexDirection: 'column', gap: 18 }}>
        <div className="field">
          <label htmlFor="name" className="lbl">NAME</label>
          <input id="name" className="input" maxLength={80} value={name} autoFocus
            aria-invalid={!!nameError} aria-describedby="name-error"
            onChange={(e) => { setName(e.target.value); setNameError('') }} />
          <FieldError id="name-error">{nameError}</FieldError>
        </div>
        <div className="field">
          <span className="lbl-row">
            <label htmlFor="code" className="lbl">CODE · OPTIONAL</label>
            <Help>The middle part of ad names: O12 in US_O12_H01. Use the code your ads in Meta already have, so their results match by themselves. Empty means the next free code.</Help>
          </span>
          <input id="code" className="input mono" maxLength={12} value={code} placeholder="e.g. O12" style={{ maxWidth: 200, textTransform: 'uppercase' }}
            aria-invalid={!!codeError} aria-describedby="code-error"
            onChange={(e) => { setCode(e.target.value); setCodeError('') }} />
          <FieldError id="code-error">{codeError}</FieldError>
        </div>
        <div className="field">
          <label htmlFor="description" className="lbl">DESCRIPTION</label>
          <textarea id="description" className="input" rows={5} maxLength={3000} value={description}
            placeholder="The ad concept, the audience and the offer. The generator reads this."
            onChange={(e) => setDescription(e.target.value)} />
        </div>
        <div className="field">
          <span className="lbl-row">
            <label htmlFor="buyer" className="lbl">BUYER · OPTIONAL</label>
            <Help>Who really buys, if it is not the person in the video: their age, situation and what bothers them. Example: women 40–60 who always run hot, or adult kids buying for a parent. The generator writes the hooks for this person.</Help>
          </span>
          <textarea id="buyer" className="input" rows={3} maxLength={1000} value={buyer}
            placeholder="Who really buys and why. Often not the person in the video."
            onChange={(e) => setBuyer(e.target.value)} />
        </div>
      </section>

      <div className="row" style={{ justifyContent: 'flex-end' }}>
        <Link to="/" className="btn btn-g">Cancel</Link>
        <button type="submit" className="btn btn-p" disabled={busy}>{busy ? 'Creating…' : 'Create project'}</button>
      </div>
    </form>
  )
}

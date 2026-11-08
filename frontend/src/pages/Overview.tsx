import { Link } from 'react-router-dom'
import { api, type ProjectSummary } from '../api'
import { money, pct } from '../format'
import { Alert } from '../ui'
import { useLoad } from '../useLoad'

const tiers = ['reference', 'good', 'anti', 'early', 'nodata'] as const

function nextStep(p: ProjectSummary) {
  if (p.hooks === 0) return { label: 'Add hooks', to: `/projects/${p.code}` }
  if (p.missingText > 0) return { label: 'Fill in hooks', to: `/projects/${p.code}?filter=notext` }
  if (p.openCandidates > 0) return { label: 'Review', to: `/projects/${p.code}/candidates` }
  return { label: 'Generate', to: `/projects/${p.code}/generate` }
}

function firstMissing(projects: ProjectSummary[]) {
  const p = projects.find((x) => x.missingText > 0)
  return p ? `/projects/${p.code}?filter=notext` : '/'
}

export function OverviewPage() {
  const { data, error } = useLoad(api.overview, [])

  if (error) return <Alert title="Could not load the overview">{error.message}</Alert>
  if (!data) return null

  const todos = [
    { n: data.openCandidates, label: 'Candidates to review', color: '#c4b5fd', to: firstWithCandidates(data.projects) },
    { n: data.missingText, label: 'Hooks without text', color: '#fdba74', to: firstMissing(data.projects) },
    { n: data.readyToExport, label: 'Hooks ready to export', color: '#7dd3fc', to: '/queue' },
  ]

  return (
    <div className="page">
      <header className="header">
        <h1>YOUR <span className="grad">WINNERS</span></h1>
        <Link to="/projects/new" className="btn btn-p">New project</Link>
      </header>

      <section aria-label="To do" className="kpis" style={{ gridTemplateColumns: 'repeat(auto-fit, minmax(240px, 1fr))' }}>
        {todos.map((t) => (
          <Link key={t.label} to={t.to} className="card todo" style={{ borderRadius: 16 }}>
            <span className="n" style={{ color: t.color }}>{t.n}</span>
            <span>{t.label}</span>
            <span aria-hidden="true" className="muted" style={{ marginLeft: 'auto' }}>→</span>
          </Link>
        ))}
      </section>

      <section aria-label="Totals" className="kpis">
        <div className="card kpi"><span className="k">Spend</span><span className="v">{money(data.spendCents, 0)}</span></div>
        <div className="card kpi"><span className="k">Avg CPA</span><span className="v">{money(data.avgCpaCents)}</span></div>
        <div className="card kpi"><span className="k">References</span><span className="v">{data.references}</span></div>
        <div className="card kpi accent">
          <span className="k">Best hook{data.bestHook && ` · ${data.bestHook.code} ${data.bestHook.number}`}</span>
          <span className="v">{money(data.bestHook?.cpaCents)}</span>
        </div>
      </section>

      <section aria-labelledby="projects-title" style={{ display: 'flex', flexDirection: 'column', gap: 14 }}>
        <h2 id="projects-title" className="section">PROJECTS</h2>
        <div className="card table-box">
          {data.projects.length === 0 ? (
            <p className="empty">No projects yet. <Link to="/projects/new">Create the first one.</Link></p>
          ) : (
            <table style={{ minWidth: 820 }}>
              <thead>
                <tr>
                  <th>PROJECT</th><th>HOOKS</th><th className="num">BEST CPA</th><th className="num">AVG CPA</th><th className="num">HOOK RATE</th>
                  <th><span className="sr-only" style={{ position: 'absolute', width: 1, height: 1, overflow: 'hidden' }}>Next step</span></th>
                </tr>
              </thead>
              <tbody>
                {data.projects.map((p) => {
                  const step = nextStep(p)
                  const label = tiers.map((t) => `${p.tiers[t]} ${t}`).join(', ')
                  return (
                    <tr key={p.code}>
                      <td><Link to={`/projects/${p.code}`} className="row-link"><span style={{ color: '#c4b5fd', marginRight: 10 }}>{p.code}</span>{p.name}</Link><div className="hint">{p.hooks} hooks</div></td>
                      <td style={{ width: 220 }}>
                        <div className="bar" role="img" aria-label={label} title={label}>
                          {tiers.map((t) => <span key={t} className={'b-' + t} style={{ flex: `${p.tiers[t]} 1 0` }} />)}
                        </div>
                      </td>
                      <td className="num strong">{money(p.bestCpaCents)}</td>
                      <td className="num">{money(p.avgCpaCents)}</td>
                      <td className="num">{pct(p.hookRate)}</td>
                      <td style={{ textAlign: 'right' }}><Link to={step.to} style={{ textDecoration: 'none', fontWeight: 700 }}>{step.label} →</Link></td>
                    </tr>
                  )
                })}
              </tbody>
            </table>
          )}
        </div>
      </section>
    </div>
  )
}

function firstWithCandidates(projects: ProjectSummary[]) {
  const p = projects.find((x) => x.openCandidates > 0) ?? projects[0]
  return p ? `/projects/${p.code}/candidates` : '/'
}

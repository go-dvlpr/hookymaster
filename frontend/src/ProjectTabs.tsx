import { NavLink } from 'react-router-dom'

const tabClass = ({ isActive }: { isActive: boolean }) => 'tab' + (isActive ? ' on' : '')

export function ProjectTabs({ code, open }: { code: string; open: number }) {
  return (
    <nav aria-label="Project" className="row" style={{ gap: 8 }}>
      <NavLink to={`/projects/${code}`} end className={tabClass}>Hooks</NavLink>
      <NavLink to={`/projects/${code}/import`} className={tabClass}>Import</NavLink>
      <NavLink to={`/projects/${code}/candidates`} className={tabClass}>
        Candidates{open > 0 && <span className="cnt violet inline">{open}</span>}
      </NavLink>
      <NavLink to={`/projects/${code}/generate`} className={tabClass}>Generate</NavLink>
      <NavLink to={`/projects/${code}/settings`} className={tabClass}>Settings</NavLink>
    </nav>
  )
}

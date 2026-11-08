import { useId, type ReactNode } from 'react'

const icons = {
  error: <path d="M10 6v5M10 14v.01M3 10a7 7 0 1 0 14 0 7 7 0 0 0-14 0z" />,
  success: <path d="M6.5 10.2 9 12.7l4.5-5M3 10a7 7 0 1 0 14 0 7 7 0 0 0-14 0z" />,
  info: <path d="M10 9v5M10 6v.01M3 10a7 7 0 1 0 14 0 7 7 0 0 0-14 0z" />,
}

// Alert is a page-level message: a short title and a plain sentence.
export function Alert({ kind = 'error', title, children, action }: {
  kind?: keyof typeof icons; title?: string; children?: ReactNode; action?: ReactNode
}) {
  const heading = title ?? (kind === 'error' ? 'Something is wrong' : undefined)
  return (
    <div className={'alert alert-' + kind} role={kind === 'error' ? 'alert' : 'status'}>
      <svg width="20" height="20" viewBox="0 0 20 20" aria-hidden="true" fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" strokeLinejoin="round">{icons[kind]}</svg>
      <div className="alert-body">
        {heading && <strong>{heading}</strong>}
        {children && <span>{children}</span>}
      </div>
      {action}
    </div>
  )
}

// FieldError sits under an input and is linked to it with aria-describedby.
export function FieldError({ id, children }: { id: string; children?: ReactNode }) {
  if (!children) return null
  return (
    <span id={id} className="field-error">
      <svg width="14" height="14" viewBox="0 0 20 20" aria-hidden="true" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round">{icons.error}</svg>
      {children}
    </span>
  )
}

// Help is a small "?" next to a label; the explanation shows on hover and on
// keyboard focus.
export function Help({ children }: { children: ReactNode }) {
  const id = useId()
  return (
    <span className="help">
      <button type="button" className="help-btn" aria-label="What is this?" aria-describedby={id}>?</button>
      <span role="tooltip" id={id} className="help-tip">{children}</span>
    </span>
  )
}

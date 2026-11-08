import { useCallback, useEffect, useState } from 'react'
import { ApiError } from './api'

// useLoad runs fn when deps change and exposes the result with a reload.
export function useLoad<T>(fn: () => Promise<T>, deps: unknown[]) {
  const [data, setData] = useState<T | null>(null)
  const [error, setError] = useState<ApiError | null>(null)
  const [tick, setTick] = useState(0)

  // eslint-disable-next-line react-hooks/exhaustive-deps
  const load = useCallback(fn, deps)

  useEffect(() => {
    let alive = true
    setError(null)
    load()
      .then((d) => alive && setData(d))
      .catch((e) => alive && setError(e instanceof ApiError ? e : new ApiError(0, String(e))))
    return () => { alive = false }
  }, [load, tick])

  return { data, error, setData, reload: () => setTick((t) => t + 1) }
}

export const errorText = (e: unknown) => (e instanceof Error ? e.message : String(e))

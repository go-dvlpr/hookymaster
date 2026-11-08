import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { BrowserRouter, Route, Routes } from 'react-router-dom'
import './styles.css'
import { Layout } from './Layout'
import { OverviewPage } from './pages/Overview'
import { NewProjectPage } from './pages/NewProject'
import { ProjectPage } from './pages/Project'
import { ImportPage } from './pages/Import'
import { GeneratePage } from './pages/Generate'
import { ReviewPage } from './pages/Review'
import { QueuePage } from './pages/Queue'
import { SettingsPage } from './pages/Settings'

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <BrowserRouter>
      <Routes>
        <Route element={<Layout />}>
          <Route index element={<OverviewPage />} />
          <Route path="projects/new" element={<NewProjectPage />} />
          <Route path="projects/:code" element={<ProjectPage />} />
          <Route path="projects/:code/generate" element={<GeneratePage />} />
          <Route path="projects/:code/candidates" element={<ReviewPage />} />
          <Route path="projects/:code/import" element={<ImportPage />} />
          <Route path="projects/:code/settings" element={<SettingsPage />} />
          <Route path="queue" element={<QueuePage />} />
          <Route path="*" element={<p className="empty">This page does not exist.</p>} />
        </Route>
      </Routes>
    </BrowserRouter>
  </StrictMode>,
)

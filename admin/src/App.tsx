import { Navigate, Route, Routes } from 'react-router-dom'
import AppShell from './components/AppShell'
import InspectionsPage from './pages/InspectionsPage'
import SystemPage from './pages/SystemPage'
import { useHealth } from './hooks/useHealth'

/**
 * health 挂在 App 层而不是页面层：顶栏徽章是两个页面共用的，
 * 切页时不该重新拉一次 health（也因此它在整个会话里是同一个轮询）。
 */
export default function App() {
  const health = useHealth()

  return (
    <Routes>
      <Route element={<AppShell health={health} />}>
        <Route path="/" element={<InspectionsPage />} />
        <Route path="/system" element={<SystemPage health={health} />} />
        <Route path="*" element={<Navigate to="/" replace />} />
      </Route>
    </Routes>
  )
}

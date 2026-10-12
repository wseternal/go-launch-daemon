import { Routes, Route, Navigate } from 'react-router-dom'
import { Layout, Typography } from 'antd'
import { Link } from 'react-router-dom'
import Services from './pages/Services'
import ServiceDetail from './pages/ServiceDetail'
import { HealthIndicator } from './components/StatusBadge'

const { Header, Content } = Layout
const { Title } = Typography

export default function App() {
  return (
    <Layout style={{ minHeight: '100vh' }}>
      <Header
        style={{
          display: 'flex',
          alignItems: 'center',
          gap: 16,
          background: '#fff',
          padding: '0 24px',
          borderBottom: '1px solid #f0f0f0',
        }}
      >
        <Title level={4} style={{ margin: 0 }}>
          <Link to="/" style={{ color: 'inherit' }}>
            Launch Daemon
          </Link>
        </Title>
        <HealthIndicator />
      </Header>
      <Content style={{ padding: 24, background: '#fafafa' }}>
        <Routes>
          <Route path="/" element={<Services />} />
          <Route path="/services/:name" element={<ServiceDetail />} />
          <Route path="*" element={<Navigate to="/" replace />} />
        </Routes>
      </Content>
    </Layout>
  )
}

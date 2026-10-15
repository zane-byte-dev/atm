import { createRoot } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { DesignSystemProvider } from '../../src/design-system'
import { AppearanceSettings } from '../../src/features/settings/appearance-settings'
import '../../src/app.css'

const client = new QueryClient()

createRoot(document.getElementById('root')!).render(
  <QueryClientProvider client={client}>
    <DesignSystemProvider>
      <div className="workspace">
        <div className="main">
          <AppearanceSettings />
        </div>
      </div>
    </DesignSystemProvider>
  </QueryClientProvider>,
)

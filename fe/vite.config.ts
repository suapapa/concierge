import react from '@vitejs/plugin-react';
import { defineConfig, loadEnv } from 'vite';

// Proxy API to Concierge during local dev so session cookies stay on `localhost`.
export default defineConfig(({ mode }) => {
  const env = loadEnv(mode, process.cwd(), '');
  const apiTarget = env.VITE_DEV_API_PROXY || 'http://127.0.0.1:8080';

  return {
    plugins: [react()],
    server: {
      proxy: {
        '/api': {
          target: apiTarget,
          changeOrigin: true,
        },
      },
    },
  };
});

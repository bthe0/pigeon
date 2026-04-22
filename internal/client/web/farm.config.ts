import { defineConfig } from '@farmfe/core';

export default defineConfig({
  plugins: ['@farmfe/plugin-react'],
  server: {
    // Proxy API calls to the pigeon web interface so `farm start` gives
    // full HMR while still hitting the live backend.
    proxy: {
      '/api': {
        target: 'http://127.0.0.1:8080',
        changeOrigin: true,
      },
    },
  },
});

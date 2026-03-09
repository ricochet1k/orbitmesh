const { spawn } = require('child_process');

const child = spawn('npm', ['run', 'dev', '--', '--host', '127.0.0.1', '--port', '4174'], {
  cwd: './frontend',
  env: {
    ...process.env,
    E2E_FRONTEND_PORT: '4174',
    VITE_API_PROXY_TARGET: 'http://127.0.0.1:8090',
  },
  stdio: 'inherit',
});

setTimeout(() => {
  console.log('Done waiting');
  child.kill();
}, 5000);

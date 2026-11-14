import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';
import './index.css';
import { App } from './App';
import { pages } from './registry';

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <App pages={pages} />
  </StrictMode>,
);

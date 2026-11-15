import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';
import './index.css';
import { App } from './App';
import { takeInviteFromAddress } from './lib/invite';
import { pages } from './registry';

// Read once, before the first render: an opened invite link (/invite#<token>) becomes the sign-up
// route, with the token kept in memory only.
const invite = takeInviteFromAddress(window.location, window.history);

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <App pages={pages} initialInvite={invite} />
  </StrictMode>,
);

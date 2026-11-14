import type { PageDef } from '../../registry';

function FakeA() {
  return <h1>Fake page A</h1>;
}

export const page: PageDef = { id: 'fake-a', path: '/fake-a', label: 'Fake A', order: 20, element: FakeA };

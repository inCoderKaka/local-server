import type { PageDef } from '../../registry';

function FakeB() {
  return <h1>Fake page B</h1>;
}

export const page: PageDef = { id: 'fake-b', path: '/fake-b', label: 'Fake B', order: 10, element: FakeB };

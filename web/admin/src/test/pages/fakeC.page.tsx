import { useParams } from '@local-server/shared';
import type { PageDef } from '../../registry';

function FakeC() {
  const { item } = useParams();
  return <h1>Fake detail {item}</h1>;
}

export const page: PageDef = { id: 'fake-c', path: '/fake-c/:item', label: 'Fake C', order: 5, element: FakeC, navVisible: false };

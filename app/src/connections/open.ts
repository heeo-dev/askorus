import { AgentClient, baseUrl } from '../api/client';
import { type Connection, ConnectionStore } from './store';

/** connect returns a saved PC and a client that carries its token, or null if it is gone. */
export async function connect(
  store: ConnectionStore,
  id: string,
): Promise<{ conn: Connection; client: AgentClient } | null> {
  const conn = await store.get(id);
  if (!conn) return null;
  const token = await store.token(id);
  return { conn, client: new AgentClient(baseUrl(conn.host, conn.port), { token: token ?? undefined }) };
}

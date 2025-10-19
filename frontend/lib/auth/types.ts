export type SessionUser = {
  sub?: string;
  name?: string | null;
  email?: string | null;
  picture?: string | null;
  created_at?: string | null;
  [key: string]: unknown;
};

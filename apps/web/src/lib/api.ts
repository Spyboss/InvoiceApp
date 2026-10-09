/**
 * Typed API client for the Hono backend.
 *
 * Type-only imports come from @erp/shared so screen code can't drift from the
 * server contract; runtime code is dependency-free (fetch + localStorage).
 */
import type { InvoiceType, Role, VehicleStatus } from '@erp/shared';

export interface User {
  id: string;
  name: string;
  email: string;
  role: Role;
}

export interface Customer {
  id: string;
  name: string;
  nic: string | null;
  phone: string | null;
  address: string | null;
}

export interface Vehicle {
  id: string;
  chassisNo: string;
  engineNo: string;
  make: string;
  model: string;
  type: string;
  color: string;
  yom: number;
  cost: string;
  price: string;
  status: VehicleStatus;
}

export interface Invoice {
  id: string;
  invoiceType: InvoiceType;
  invoiceNo: string;
  year: number;
  date: string;
  customerId: string;
  financeCompany: string | null;
  financeAddress: string | null;
  delivery: string | null;
  dealer: string | null;
  vehicleId: string | null;
  model: string | null;
  engineNo: string | null;
  chassisNo: string | null;
  color: string | null;
  vehicleType: string | null;
  yom: number | null;
  price: string;
  down: string;
  balance: string;
  status: string;
}

export interface InvoiceWithCustomer extends Invoice {
  customer: Customer;
}

export interface InvoiceCreateInput {
  invoiceType: InvoiceType;
  date: string;
  customerId: string;
  vehicleId?: string;
  model?: string;
  engineNo?: string;
  chassisNo?: string;
  color?: string;
  vehicleType?: string;
  yom?: number;
  dealer?: string;
  financeCompany?: string;
  financeAddress?: string;
  delivery?: string;
  price: string;
  down: string;
}

export class ApiError extends Error {
  constructor(
    public status: number,
    message: string,
  ) {
    super(message);
  }
}

// ------------------------------------------------------------- token store
const TOKEN_KEY = 'erp.token';
const USER_KEY = 'erp.user';

export const session = {
  getToken(): string | null {
    return localStorage.getItem(TOKEN_KEY);
  },
  set(token: string, user: User) {
    localStorage.setItem(TOKEN_KEY, token);
    localStorage.setItem(USER_KEY, JSON.stringify(user));
  },
  getUser(): User | null {
    try {
      const raw = localStorage.getItem(USER_KEY);
      return raw ? (JSON.parse(raw) as User) : null;
    } catch {
      return null;
    }
  },
  clear() {
    localStorage.removeItem(TOKEN_KEY);
    localStorage.removeItem(USER_KEY);
  },
};

// ---------------------------------------------------------------- requests
async function request<T>(path: string, init: RequestInit = {}): Promise<T> {
  const token = session.getToken();
  const res = await fetch(path, {
    ...init,
    headers: {
      'Content-Type': 'application/json',
      ...(token ? { Authorization: `Bearer ${token}` } : {}),
      ...init.headers,
    },
  });
  const body: unknown = await res.json().catch(() => null);
  if (!res.ok) {
    const err = (body as { error?: unknown } | null)?.error;
    const msg =
      typeof err === 'string'
        ? err
        : err
          ? JSON.stringify(err)
          : `HTTP ${res.status}`;
    throw new ApiError(res.status, msg);
  }
  return body as T;
}

const qs = (params: Record<string, string | undefined>) => {
  const parts = Object.entries(params)
    .filter(([, v]) => v)
    .map(([k, v]) => `${k}=${encodeURIComponent(v!)}`);
  return parts.length ? `?${parts.join('&')}` : '';
};

// ------------------------------------------------------------------ api
export const api = {
  // auth
  login: (email: string, password: string) =>
    request<{ token: string; user: User }>('/api/auth/login', {
      method: 'POST',
      body: JSON.stringify({ email, password }),
    }),
  register: (data: { name: string; email: string; password: string }) =>
    request<{ token: string; user: User }>('/api/auth/register', {
      method: 'POST',
      body: JSON.stringify(data),
    }),

  // CRM
  customers: (q?: string) =>
    request<{ customers: Customer[] }>(`/api/customers${qs({ q })}`),
  createCustomer: (data: { name: string; nic?: string; phone?: string; address?: string }) =>
    request<{ customer: Customer }>('/api/customers', {
      method: 'POST',
      body: JSON.stringify(data),
    }),

  // stock
  vehicles: (status?: VehicleStatus) =>
    request<{ vehicles: Vehicle[] }>(`/api/vehicles${qs({ status })}`),

  // invoices
  invoices: (type?: InvoiceType) =>
    request<{ invoices: InvoiceWithCustomer[] }>(`/api/invoices${qs({ type })}`),
  createInvoice: (data: InvoiceCreateInput) =>
    request<{ invoice: Invoice }>('/api/invoices', {
      method: 'POST',
      body: JSON.stringify(data),
    }),
  async invoicePdf(id: string): Promise<{ blob: Blob; filename: string }> {
    const token = session.getToken();
    const res = await fetch(`/api/invoices/${id}/pdf`, {
      headers: token ? { Authorization: `Bearer ${token}` } : {},
    });
    if (!res.ok) {
      const body: unknown = await res.json().catch(() => null);
      const err = (body as { error?: unknown } | null)?.error;
      throw new ApiError(res.status, typeof err === 'string' ? err : `HTTP ${res.status}`);
    }
    const disposition = res.headers.get('Content-Disposition') ?? '';
    const match = disposition.match(/filename="([^"]+)"/);
    return { blob: await res.blob(), filename: match?.[1] ?? 'invoice.pdf' };
  },
};

// -------------------------------------------------------------- downloads
export function downloadBlob(blob: Blob, filename: string): void {
  const url = URL.createObjectURL(blob);
  const link = document.createElement('a');
  link.href = url;
  link.setAttribute('download', filename);
  document.body.appendChild(link);
  link.click();
  link.remove();
  URL.revokeObjectURL(url);
}

import type { Page } from "@playwright/test";

const API_BASE = "http://localhost:8080";

export interface RegisteredTenant {
  businessName: string;
  email: string;
  password: string;
  token: string;
}

/** Registers a fresh tenant through the real UI form and leaves `page` logged in on /dashboard. */
export async function registerNewTenant(page: Page): Promise<RegisteredTenant> {
  const suffix = `${Date.now()}-${Math.floor(Math.random() * 100000)}`;
  const businessName = `E2E Test Store ${suffix}`;
  const email = `e2e-${suffix}@example.com`;
  const password = "Test1234!";

  await page.goto("/auth/register");
  await page.getByLabel("Nombre de la empresa").fill(businessName);
  await page.getByLabel("País").selectOption("VE");
  await page.getByLabel("Email del propietario").fill(email);
  await page.getByLabel("Contraseña", { exact: true }).fill(password);
  await page.getByLabel("Confirmar contraseña").fill(password);
  await page.getByRole("button", { name: "Siguiente" }).click();

  await page.getByLabel(/ID Fiscal/).fill("J-12345678-9");
  await page.getByRole("button", { name: "Crear cuenta" }).click();

  await page.waitForURL("**/dashboard");

  const token = await page.evaluate(() => localStorage.getItem("daas_token"));
  if (!token)
    throw new Error("registration did not persist daas_token in localStorage");

  return { businessName, email, password, token };
}

/** Creates a non-fiscal product with stock via the real API, using an already-registered tenant's token. */
export async function createSellableProduct(
  page: Page,
  token: string,
  overrides: {
    name?: string;
    barcode?: string;
    internalPrice?: number;
    stock?: number;
  } = {},
): Promise<{ id: string; barcode: string }> {
  const suffix = `${Date.now()}-${Math.floor(Math.random() * 100000)}`;
  const name = overrides.name ?? `E2E Product ${suffix}`;
  const barcode = overrides.barcode ?? `E2E-${suffix}`;
  const internalPrice = overrides.internalPrice ?? 10;
  const stock = overrides.stock ?? 50;

  const createRes = await page.request.post(`${API_BASE}/api/v1/products`, {
    headers: { Authorization: `Bearer ${token}` },
    data: {
      name,
      barcode,
      is_fiscal: false,
      internal_price: internalPrice,
    },
  });
  if (!createRes.ok()) {
    throw new Error(
      `product creation failed: ${createRes.status()} ${await createRes.text()}`,
    );
  }
  const { product } = await createRes.json();

  const adjustRes = await page.request.post(
    `${API_BASE}/api/v1/inventory/adjustments`,
    {
      headers: { Authorization: `Bearer ${token}` },
      data: {
        product_id: product.id,
        delta: stock,
        notes: "E2E seed stock",
      },
    },
  );
  if (!adjustRes.ok()) {
    throw new Error(
      `stock adjustment failed: ${adjustRes.status()} ${await adjustRes.text()}`,
    );
  }

  return { id: product.id, barcode };
}

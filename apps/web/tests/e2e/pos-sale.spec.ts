import { test, expect } from "@playwright/test";
import { registerNewTenant, createSellableProduct } from "./helpers/auth";

test("POS blocks sales with no open cash session", async ({ page }) => {
  await registerNewTenant(page);

  await page.goto("/pos");
  await expect(page.getByText("Abrí la caja antes de vender")).toBeVisible();
  await expect(page.getByRole("link", { name: "Ir a Caja" })).toHaveAttribute(
    "href",
    "/cash-sessions",
  );
});

test("complete a quick sale by barcode with an open cash session", async ({
  page,
}) => {
  const tenant = await registerNewTenant(page);
  const product = await createSellableProduct(page, tenant.token, {
    internalPrice: 12.5,
    stock: 20,
  });

  await page.goto("/cash-sessions");
  await page.getByLabel(/Tasa manual BS\/USD/).fill("45");
  await page.getByRole("button", { name: "Abrir caja" }).click();
  await expect(page.getByText("Caja abierta")).toBeVisible();

  await page.goto("/pos");
  const barcodeInput = page.getByPlaceholder("Escaneá el código de barras...");
  await expect(barcodeInput).toBeVisible();
  await barcodeInput.fill(product.barcode);
  await barcodeInput.press("Enter");

  await expect(page.getByText("1 ítem")).toBeVisible();
  await expect(page.getByText("$12.50")).toBeVisible();

  await page.getByRole("button", { name: "Efectivo" }).click();
  await page.getByRole("button", { name: "Cobrar" }).click();

  await expect(page.getByText(/Venta registrada por \$12\.50/)).toBeVisible();
});

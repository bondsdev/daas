import { test, expect } from "@playwright/test";
import { registerNewTenant } from "./helpers/auth";

test("open a cash session with a manual exchange rate", async ({ page }) => {
  await registerNewTenant(page);

  await page.goto("/cash-sessions");
  await expect(page.getByText("La caja está cerrada")).toBeVisible();

  await page.getByLabel(/Tasa manual BS\/USD/).fill("45.50");
  await page.getByRole("button", { name: "Abrir caja" }).click();

  await expect(page.getByText("Caja abierta")).toBeVisible();
  await expect(page.getByText(/45\.50/)).toBeVisible();
  await expect(page.getByText(/manual/)).toBeVisible();
});

test("close a cash session and see the closing summary", async ({ page }) => {
  await registerNewTenant(page);

  await page.goto("/cash-sessions");
  await page.getByLabel(/Tasa manual BS\/USD/).fill("40");
  await page.getByRole("button", { name: "Abrir caja" }).click();
  await expect(page.getByText("Caja abierta")).toBeVisible();

  await page.getByRole("button", { name: "Cerrar caja" }).click();
  await expect(
    page.getByRole("heading", { name: "Cerrar caja" }),
  ).toBeVisible();
  await page.getByRole("button", { name: "Confirmar cierre" }).click();

  await expect(page.getByText("Resumen del cierre")).toBeVisible();
  await expect(page.getByText("Total BS")).toBeVisible();
  await expect(page.getByText("Total USD")).toBeVisible();
  await expect(page.getByText("Sin ventas registradas")).toBeVisible();
});

import { test } from "@playwright/test";

// E2E stubs — full scenarios implemented in Sprint 5
// when the full app is running end-to-end with a real backend.
//
// Covers: T6-riders acceptance criteria

test("riders page — shows disabled empty state when delivery is not enabled for tenant", async () => {
  test.skip(true, "TODO Sprint 5");
});
test("rider create — requires an existing tenant_users.id to create a rider", async () => {
  test.skip(true, "TODO Sprint 5");
});
test("rider edit — toggling active deactivates the rider", async () => {
  test.skip(true, "TODO Sprint 5");
});
test("order detail — assign rider manually attaches rider_id to the order", async () => {
  test.skip(true, "TODO Sprint 5");
});
test("order detail — auto-assign rider picks an eligible available rider", async () => {
  test.skip(true, "TODO Sprint 5");
});
test("order detail — mark delivered sets rider_delivered_at and hides the action", async () => {
  test.skip(true, "TODO Sprint 5");
});
test("order detail — mark rider paid sets rider_paid_at and hides the action", async () => {
  test.skip(true, "TODO Sprint 5");
});
test("order detail — urgent toggle calls the urgent PATCH endpoint", async () => {
  test.skip(true, "TODO Sprint 5");
});

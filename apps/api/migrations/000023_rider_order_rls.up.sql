-- Rider-scoped RLS backstop for shop_orders.
--
-- The existing tenant_isolation policy on shop_orders (migration 000010) is
-- FOR ALL TO authenticated with no role distinction — it grants every
-- authenticated tenant member (owner, employee, rider) full tenant-wide
-- access. A second PERMISSIVE policy would be OR'd with it and therefore
-- could never narrow what a rider sees. A RESTRICTIVE policy is AND'd with
-- permissive policies instead, so it can narrow access without touching the
-- existing policy or owner/employee behavior.
--
-- Scoped to FOR SELECT only, matching the task's read-only "see my own
-- deliveries" requirement. This does NOT restrict INSERT/UPDATE/DELETE for a
-- rider session — those remain governed solely by tenant_isolation, same as
-- an employee session today. That is a pre-existing gap (tenant_isolation
-- has never distinguished roles for writes either), not one introduced here;
-- closing it would mean redesigning tenant_isolation for every role across
-- every table, which is out of scope for this backstop.
--
-- app.rider_id is set by TenantMiddleware only when the session's role is
-- 'rider' (NULL/unset for owner/employee). current_setting(...) IS NULL is
-- therefore the signal "this is not a rider session" — if a rider session
-- somehow has no matching riders row, app.rider_id also resolves to NULL and
-- this policy would NOT restrict them. That edge case cannot occur today
-- (CreateRider always creates the riders row before a 'rider' role can
-- exist), but is called out here since it depends on that invariant holding.
CREATE POLICY rider_order_scope ON shop_orders
    AS RESTRICTIVE
    FOR SELECT
    TO authenticated
    USING (
        current_setting('app.rider_id', true) IS NULL
        OR rider_id = current_setting('app.rider_id', true)::UUID
    );

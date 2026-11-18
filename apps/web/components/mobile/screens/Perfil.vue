<script setup lang="ts">
import { AlertCircle, HelpCircle, Loader2, LogOut } from "lucide-vue-next";

export interface RiderProfile {
  name: string;
  phone: string;
  rate_per_delivery: number;
  max_concurrent_deliveries: number;
  status: "available" | "unavailable";
}

const props = defineProps<{
  profile: RiderProfile | null;
  earningsThisWeek: number;
  deliveriesThisWeek: number;
  pendingPayout: number;
  loading: boolean;
  loadError: string;
  statusError: string;
  togglingStatus: boolean;
}>();

const emit = defineEmits<{ toggleStatus: []; logout: [] }>();

function fmtCurrency(n: number) {
  return new Intl.NumberFormat("es-VE", {
    minimumFractionDigits: 2,
    maximumFractionDigits: 2,
  }).format(n);
}

const initials = computed(() => {
  if (!props.profile?.name) return "";
  return props.profile.name
    .split(/\s+/)
    .filter(Boolean)
    .slice(0, 2)
    .map((part) => part[0]!.toUpperCase())
    .join("");
});

const isAvailable = computed(() => props.profile?.status === "available");
</script>

<template>
  <MobileScreen variant="brand">
    <template #header>
      <header
        class="shrink-0 bg-primary px-5 pb-[22px] pt-4 rounded-b-3xl"
        style="padding-top: calc(1rem + env(safe-area-inset-top))"
      >
        <div class="flex items-center gap-3.5">
          <div
            class="flex size-14 shrink-0 items-center justify-center rounded-2xl bg-white/[0.18] text-[22px] font-bold text-white"
          >
            {{ initials }}
          </div>
          <div class="min-w-0">
            <h1 class="truncate text-[17px] font-bold text-white">
              {{ profile?.name ?? "—" }}
            </h1>
            <p class="truncate text-[12.5px] text-white/85">Motorizado</p>
          </div>
        </div>
      </header>
    </template>

    <div
      v-if="loadError"
      class="mx-4 mt-3 rounded-xl border border-destructive/30 bg-destructive/10 px-4 py-3 text-sm text-destructive"
    >
      {{ loadError }}
    </div>

    <div v-else-if="loading && !profile" class="flex justify-center py-16">
      <Loader2 class="size-6 animate-spin text-muted-foreground" />
    </div>

    <template v-else-if="profile">
      <div
        class="relative z-10 -mt-3.5 mx-4 flex items-center justify-between rounded-xl border border-border bg-white px-4 py-3.5 shadow-[0_4px_16px_rgba(0,0,0,0.08)]"
      >
        <div class="flex items-center gap-2">
          <span
            class="size-[9px] shrink-0 rounded-full"
            :class="isAvailable ? 'bg-emerald-400' : 'bg-muted-foreground'"
          />
          <div>
            <p class="text-[13.5px] font-bold leading-tight">
              {{ isAvailable ? "Disponible para entregas" : "No disponible" }}
            </p>
            <p class="mt-px text-[11.5px] text-muted-foreground">
              Te asignamos pedidos nuevos mientras estés en línea
            </p>
          </div>
        </div>

        <button
          type="button"
          role="switch"
          :aria-checked="isAvailable"
          aria-label="Disponible para entregas"
          :disabled="togglingStatus"
          class="relative flex size-11 shrink-0 items-center justify-center disabled:opacity-60"
          @click="emit('toggleStatus')"
        >
          <span
            class="relative h-[27px] w-[46px] rounded-full transition-colors"
            :class="isAvailable ? 'bg-emerald-400' : 'bg-border'"
          >
            <span
              class="absolute top-[3px] size-[21px] rounded-full bg-white shadow transition-[left]"
              :class="isAvailable ? 'left-[22px]' : 'left-[3px]'"
            />
          </span>
        </button>
      </div>

      <p
        v-if="statusError"
        class="mx-4 mt-2 flex items-center gap-1.5 text-xs text-destructive"
      >
        <AlertCircle class="size-3.5" /> {{ statusError }}
      </p>

      <section class="px-5 pt-[18px]">
        <p
          class="mb-2.5 text-xs font-bold uppercase tracking-wide text-muted-foreground"
        >
          Esta semana
        </p>
        <div class="flex gap-2.5">
          <div
            class="flex-1 rounded-xl border border-border bg-white px-3.5 py-3"
          >
            <p class="text-[11px] font-semibold text-muted-foreground">
              Ganado
            </p>
            <p class="mt-0.5 font-mono text-[17px] font-bold text-primary">
              {{ fmtCurrency(earningsThisWeek) }}
            </p>
          </div>
          <div
            class="flex-1 rounded-xl border border-border bg-white px-3.5 py-3"
          >
            <p class="text-[11px] font-semibold text-muted-foreground">
              Entregas
            </p>
            <p class="mt-0.5 font-mono text-[17px] font-bold text-primary">
              {{ deliveriesThisWeek }}
            </p>
          </div>
          <div
            class="flex-1 rounded-xl border border-border bg-white px-3.5 py-3"
          >
            <p class="text-[11px] font-semibold text-muted-foreground">
              Por liquidar
            </p>
            <p class="mt-0.5 font-mono text-[17px] font-bold text-primary">
              {{ fmtCurrency(pendingPayout) }}
            </p>
          </div>
        </div>
      </section>

      <section class="px-5 pt-[18px]">
        <p
          class="mb-2.5 text-xs font-bold uppercase tracking-wide text-muted-foreground"
        >
          Mis datos
        </p>
        <div
          class="divide-y divide-border rounded-xl border border-border bg-white"
        >
          <div
            class="flex min-h-11 items-center justify-between gap-2.5 px-3.5 py-3"
          >
            <span class="text-[13px] text-muted-foreground">Teléfono</span>
            <span class="font-mono text-[13.5px] font-semibold">{{
              profile.phone
            }}</span>
          </div>
          <div
            class="flex min-h-11 items-center justify-between gap-2.5 px-3.5 py-3"
          >
            <span class="text-[13px] text-muted-foreground"
              >Tarifa por entrega</span
            >
            <span class="font-mono text-[13.5px] font-semibold">
              {{ fmtCurrency(profile.rate_per_delivery) }}
            </span>
          </div>
          <div
            class="flex min-h-11 items-center justify-between gap-2.5 px-3.5 py-3"
          >
            <span class="text-[13px] text-muted-foreground"
              >Entregas simultáneas</span
            >
            <span class="font-mono text-[13.5px] font-semibold">
              máx. {{ profile.max_concurrent_deliveries }}
            </span>
          </div>
        </div>
      </section>

      <section class="px-5 py-[18px]">
        <p
          class="mb-2.5 text-xs font-bold uppercase tracking-wide text-muted-foreground"
        >
          Cuenta
        </p>
        <div
          class="divide-y divide-border rounded-xl border border-border bg-white"
        >
          <!-- ponytail: out of scope for this task, inert placeholder only. -->
          <button
            type="button"
            disabled
            class="flex min-h-11 w-full items-center gap-3 px-3.5 py-3 text-left text-[13.5px] font-semibold text-foreground opacity-60"
          >
            <HelpCircle class="size-[18px] shrink-0 text-muted-foreground" />
            Ayuda y soporte
          </button>
          <button
            type="button"
            class="no-min-tap flex min-h-11 w-full items-center gap-3 px-3.5 py-3 text-left text-[13.5px] font-semibold text-destructive"
            @click="emit('logout')"
          >
            <LogOut class="size-[18px] shrink-0 text-destructive" />
            Cerrar sesión
          </button>
        </div>
      </section>
    </template>
  </MobileScreen>
</template>

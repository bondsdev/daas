<script setup lang="ts">
import { LayoutGrid, History, User } from "lucide-vue-next";

const route = useRoute();

interface TabItem {
  label: string;
  icon: Component;
  to: string;
  match: string;
}

const tabs: TabItem[] = [
  {
    label: "Entregas",
    icon: LayoutGrid,
    to: "/my-deliveries",
    match: "/my-deliveries",
  },
  {
    label: "Historial",
    icon: History,
    to: "/my-deliveries/historial",
    match: "/my-deliveries/historial",
  },
  {
    label: "Perfil",
    icon: User,
    to: "/my-deliveries/perfil",
    match: "/my-deliveries/perfil",
  },
];

function isTabActive(tab: TabItem) {
  return route.path === tab.match;
}
</script>

<template>
  <!--
    Rider-exclusive bottom navigation — mirrors BottomNav.vue's fixed-overlay
    technique so it integrates with MobileScreen's reserved scroll padding.
  -->
  <nav
    class="fixed bottom-0 inset-x-0 z-50 bg-white border-t border-border"
    style="padding-bottom: env(safe-area-inset-bottom)"
  >
    <div class="flex items-end h-[60px]">
      <NuxtLink
        v-for="tab in tabs"
        :key="tab.to"
        :to="tab.to"
        class="flex flex-1 flex-col items-center justify-center gap-[3px] py-2 relative"
      >
        <span
          v-if="isTabActive(tab)"
          class="absolute top-2 w-12 h-7 rounded-full bg-primary/10 transition-all duration-200"
          aria-hidden="true"
        />

        <component
          :is="tab.icon"
          class="size-[22px] relative z-10 transition-colors duration-150"
          :class="
            isTabActive(tab)
              ? 'text-primary stroke-[2.5]'
              : 'text-muted-foreground/70 stroke-[1.75]'
          "
        />

        <span
          class="text-[10px] relative z-10 leading-none transition-colors duration-150"
          :class="
            isTabActive(tab)
              ? 'text-primary font-bold'
              : 'text-muted-foreground/60 font-medium'
          "
        >
          {{ tab.label }}
        </span>
      </NuxtLink>
    </div>
  </nav>
</template>

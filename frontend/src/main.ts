import { createApp } from "vue";
import App from "./App.vue";

import { OhVueIcon, addIcons } from "oh-vue-icons";
import { BiTrashFill, MdModeeditoutline, HiRefresh, CoSad } from "oh-vue-icons/icons";

const app = createApp(App);
app.component("v-icon", OhVueIcon);
addIcons(BiTrashFill, MdModeeditoutline, HiRefresh, CoSad);
app.mount("#app");

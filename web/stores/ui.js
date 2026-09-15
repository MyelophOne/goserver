import { createStore } from 'zustand/vanilla';

export const store = createStore((set) => ({
	modal: null,
	commandPaletteOpen: false,
	searchOpen: false,
	language: '',
	openModal(id) {
		set({ modal: id });
	},
	closeModal() {
		set({ modal: null });
	},
	setCommandPaletteOpen(value) {
		set({ commandPaletteOpen: !!value });
	},
	setSearchOpen(value) {
		set({ searchOpen: !!value });
	},
	setLanguage(language) {
		set({ language: String(language || '') });
	},
}));

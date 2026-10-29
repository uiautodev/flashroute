<script>
	import ArrowDown from '@lucide/svelte/icons/arrow-down';
	import ArrowUp from '@lucide/svelte/icons/arrow-up';
	import Check from '@lucide/svelte/icons/check';
	import ChevronDown from '@lucide/svelte/icons/chevron-down';
	import ChevronLeft from '@lucide/svelte/icons/chevron-left';
	import ChevronRight from '@lucide/svelte/icons/chevron-right';
	import ChevronUp from '@lucide/svelte/icons/chevron-up';
	import Clock from '@lucide/svelte/icons/clock';
	import Copy from '@lucide/svelte/icons/copy';
	import ExternalLink from '@lucide/svelte/icons/external-link';
	import Globe from '@lucide/svelte/icons/globe';
	import ImageIcon from '@lucide/svelte/icons/image';
	import LayoutGrid from '@lucide/svelte/icons/layout-grid';
	import List from '@lucide/svelte/icons/list';
	import KeyRound from '@lucide/svelte/icons/key-round';
	import Lock from '@lucide/svelte/icons/lock';
	import LogOut from '@lucide/svelte/icons/log-out';
	import Menu from '@lucide/svelte/icons/menu';
	import MessageSquare from '@lucide/svelte/icons/message-square';
	import Monitor from '@lucide/svelte/icons/monitor';
	import Moon from '@lucide/svelte/icons/moon';
	import Pencil from '@lucide/svelte/icons/pencil';
	import Plus from '@lucide/svelte/icons/plus';
	import RefreshCw from '@lucide/svelte/icons/refresh-cw';
	import Route from '@lucide/svelte/icons/route';
	import Send from '@lucide/svelte/icons/send';
	import Server from '@lucide/svelte/icons/server';
	import Settings from '@lucide/svelte/icons/settings';
	import ShieldCheck from '@lucide/svelte/icons/shield-check';
	import Sparkles from '@lucide/svelte/icons/sparkles';
	import Sun from '@lucide/svelte/icons/sun';
	import Trash from '@lucide/svelte/icons/trash';
	import TriangleAlert from '@lucide/svelte/icons/triangle-alert';
	import X from '@lucide/svelte/icons/x';
	import Zap from '@lucide/svelte/icons/zap';

	export let name = 'spark';
	export let size = 18;
	export let strokeWidth = 1.8;

	const icons = {
		alert: TriangleAlert,
		'arrow-down': ArrowDown,
		'arrow-up': ArrowUp,
		chat: MessageSquare,
		check: Check,
		'chevron-down': ChevronDown,
		'chevron-left': ChevronLeft,
		'chevron-right': ChevronRight,
		'chevron-up': ChevronUp,
		clock: Clock,
		copy: Copy,
		edit: Pencil,
		external: ExternalLink,
		globe: Globe,
		image: ImageIcon,
		grid: LayoutGrid,
		list: List,
		key: KeyRound,
		lock: Lock,
		logout: LogOut,
		menu: Menu,
		monitor: Monitor,
		moon: Moon,
		plus: Plus,
		refresh: RefreshCw,
		route: Route,
		send: Send,
		server: Server,
		settings: Settings,
		shield: ShieldCheck,
		spark: Sparkles,
		sun: Sun,
		trash: Trash,
		x: X,
		zap: Zap
	};

	$: Component = icons[name] || Sparkles;
</script>

<Component {size} {strokeWidth} aria-hidden="true" />

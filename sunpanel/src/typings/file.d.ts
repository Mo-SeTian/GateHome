declare namespace File {

	interface Info extends Common.InfoBase {
		references?: { title: string; kind: string }[] | null
		size?: number
		src: string
		userId: number
		fileName: string
		method: number
		ext: string
	}


}
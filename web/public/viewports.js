export const VIEWPORT_PRESETS = [
  { id:'desktop', group:'Desktop', width:1280, height:800, dpr:1, mobile:false, touch:false },
  { id:'desktop-fullhd', group:'Desktop', width:1920, height:1080, dpr:1, mobile:false, touch:false },
  { id:'tablet', group:'Tablet', width:768, height:1024, dpr:1, mobile:false, touch:true },
  { id:'tablet-large', group:'Tablet', width:1536, height:2048, dpr:1, mobile:false, touch:true },
  { id:'mobile', group:'Mobile', width:393, height:852, dpr:1, mobile:true, touch:true },
  { id:'mobile-fullhd', group:'Mobile', width:1080, height:1920, dpr:1, mobile:true, touch:true },
];

export function viewportArguments(id) {
  const preset = VIEWPORT_PRESETS.find(value => value.id === id);
  if (!preset) throw new Error('Scegli una risoluzione del browser.');
  const { width, height, dpr, mobile, touch } = preset;
  return { width, height, dpr, mobile, touch };
}

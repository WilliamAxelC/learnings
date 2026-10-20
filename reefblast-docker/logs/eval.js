(() => {
  const S = window.__S;
  return {
    me: S?.me ? { name: S.me.name, level: S.me.level, coins: S.me.coins, itemsCount: S.me.items?.length, boat: S.me.boat, helper: S.me.helper, equipped: S.me.equipped } : null,
    zone: S?.zone?.zone ?? 'Harbor',
    marketListings: S?.market?.listings?.map(l => ({ id: l.id, typeOfId: typeof l.id, price: l.price, itemW: l.item?.w, seller: l.seller })),
    failedListings: window.__ReefBlastBot?._failedMarketListings,
    bestMarketDeal: window.__ReefBlastBot?.bestMarketDeal
  };
})()

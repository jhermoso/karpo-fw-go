// Organigrama (T7): listado `role=tree` y diagrama @xyflow + dagre sobre un modelo de árbol común.
import './tree.css';

export { TreeView, treeLayoutOf, type TreeLayout } from './TreeView';
export {
  buildFromChart,
  buildFromFlat,
  countDescendants,
  findMatches,
  flattenVisible,
  mergeChart,
  parseChart,
  type ChartTree,
  type Descendants,
  type TreeModel,
  type TreeNode,
  type TreeViewDef,
  type VisibleItem,
} from './model';
export { layoutTree, type LayoutEdge, type LayoutNode, type LayoutOptions, type LayoutResult } from './layout';
export { diagramKeyAction, treeKeyAction, typeAheadTarget, type TreeAction } from './keyboard';

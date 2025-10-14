import cubejsApi from "../utils/cubejsApi.js";
import { fetchGraphQL } from "../utils/graphql.js";
import logger from "../utils/logger.js";
import { updatePlaygroundState } from "../utils/playgroundState.js";

const explorationQuery = `
  query ($id: uuid!) {
    explorations_by_pk(id: $id) {
      id
      branch_id
      datasource_id
      playground_state
    }
  }
`;

export const fetchData = async (exploration, args, authToken) => {
  let { playground_state: playgroundState } = exploration;

  const {
    userId,
    renewQuery = true,
    validateMeta = true,
    format = "json",
    limit,
    offset,
  } = args || {};

  logger.info(`[fetchData] Starting data fetch`, {
    explorationId: exploration.id,
    dataSourceId: exploration.datasource_id,
    branchId: exploration.branch_id,
    userId,
    limit,
    offset,
    renewQuery,
    validateMeta,
    format,
  });

  if (limit) {
    playgroundState.limit = limit;
  }

  if (offset) {
    playgroundState.offset = offset;
  }

  logger.info(`[fetchData] Playground state prepared`, {
    measures: playgroundState.measures,
    dimensions: playgroundState.dimensions,
    timeDimensions: playgroundState.timeDimensions?.map(td => td.dimension),
    filters: playgroundState.filters?.length || 0,
    limit: playgroundState.limit,
    offset: playgroundState.offset,
  });

  const cubejs = cubejsApi({
    dataSourceId: exploration.datasource_id,
    branchId: exploration.branch_id,
    userId: userId,
    authToken,
  });

  logger.info(`[fetchData] CubeJS API client initialized`, {
    dataSourceId: exploration.datasource_id,
    branchId: exploration.branch_id,
  });

  let updatedPlaygroundState = playgroundState;
  let skippedMembers = [];

  if (validateMeta) {
    logger.info(`[fetchData] Fetching metadata from CubeJS...`);
    const metaStartTime = Date.now();

    try {
      const meta = await cubejs.meta();

      logger.info(`[fetchData] Metadata fetched successfully`, {
        duration: Date.now() - metaStartTime,
        cubesCount: meta?.length || 0,
        cubeNames: meta?.map(c => c.name) || [],
      });

      const normalizedMetaState = updatePlaygroundState(playgroundState, meta);

      updatedPlaygroundState = normalizedMetaState.updatedPlaygroundState;
      skippedMembers = normalizedMetaState.skippedMembers;

      if (skippedMembers.length > 0) {
        logger.warn(`[fetchData] Some members were skipped during validation`, {
          skippedMembers,
        });
      }
    } catch (metaErr) {
      logger.error(`[fetchData] Failed to fetch metadata from CubeJS`, {
        duration: Date.now() - metaStartTime,
        error: metaErr.message || metaErr,
        stack: metaErr.stack,
        errorResponse: metaErr.response,
      });
      throw metaErr;
    }
  }

  logger.info(`[fetchData] Executing CubeJS query...`, {
    cubeJsUrl: process.env.CUBEJS_URL || "http://cubejs:4000",
    renewQuery,
    query: {
      measures: updatedPlaygroundState.measures,
      dimensions: updatedPlaygroundState.dimensions,
      timeDimensions: updatedPlaygroundState.timeDimensions?.map(td => ({
        dimension: td.dimension,
        granularity: td.granularity,
      })),
      limit: updatedPlaygroundState.limit,
      offset: updatedPlaygroundState.offset,
    },
  });

  const queryStartTime = Date.now();

  try {
    const cubeData = await cubejs.query(updatedPlaygroundState, format, {
      renewQuery,
    });

    logger.info(`[fetchData] CubeJS query completed successfully`, {
      duration: Date.now() - queryStartTime,
      dataLength: cubeData?.data?.length || 0,
      hitLimit: cubeData?.hitLimit,
      hasAnnotation: !!cubeData?.annotation,
      query: cubeData?.query,
    });

    logger.info(`[fetchData] CubeJS response data sample`, {
      firstRow: cubeData?.data?.[0] || null,
      totalRows: cubeData?.data?.length || 0,
    });

    return {
      ...cubeData,
      annotation: {
        ...cubeData.annotation,
        skippedMembers,
      },
    };
  } catch (queryErr) {
    logger.error(`[fetchData] CubeJS query failed`, {
      duration: Date.now() - queryStartTime,
      error: queryErr.message || queryErr,
      stack: queryErr.stack,
      progressResponse: queryErr?.progressResponse,
      errorResponse: queryErr.response,
      query: updatedPlaygroundState,
    });
    throw queryErr;
  }
};

export default async (session, input, headers) => {
  const { exploration_id: explorationId, limit, offset } = input || {};
  const userId = session?.["x-hasura-user-id"];
  const authToken = headers?.authorization;

  logger.info(`[fetchDataset] RPC called`, {
    explorationId,
    userId,
    limit,
    offset,
    hasAuthToken: !!authToken,
  });

  try {
    logger.info(`[fetchDataset] Fetching exploration from GraphQL`, {
      explorationId,
    });

    const exploration = await fetchGraphQL(
      explorationQuery,
      { id: explorationId },
      authToken
    );

    if (!exploration?.data?.explorations_by_pk) {
      logger.error(`[fetchDataset] Exploration not found`, {
        explorationId,
        response: exploration,
      });
      throw new Error(`Exploration ${explorationId} not found`);
    }

    logger.info(`[fetchDataset] Exploration fetched successfully`, {
      explorationId,
      dataSourceId: exploration.data.explorations_by_pk.datasource_id,
      branchId: exploration.data.explorations_by_pk.branch_id,
    });

    const result = await fetchData(
      exploration?.data?.explorations_by_pk,
      {
        userId,
        limit,
        offset,
      },
      authToken
    );

    logger.info(`[fetchDataset] RPC completed successfully`, {
      explorationId,
      dataLength: result?.data?.length || 0,
    });

    return result;
  } catch (err) {
    logger.error(`[fetchDataset] RPC error occurred`, {
      explorationId,
      userId,
      error: err.message || err,
      stack: err.stack,
      progressResponse: err?.progressResponse,
    });

    const isContinueWait = err?.progressResponse?.error;

    let progress = {
      loading: false,
    };

    if (isContinueWait) {
      logger.info(`[fetchDataset] Query in progress, continue waiting`, {
        stage: err?.progressResponse?.stage,
      });

      progress = {
        loading: true,
        ...err?.progressResponse?.stage,
      };
    } else {
      const errMessage = err.message || err;

      if (errMessage) {
        progress.error = errMessage;
      }
    }

    return {
      annotation: {
        skippedMembers: [],
        measures: {},
        dimensions: {},
        timeDimensions: {},
        segments: {},
      },
      data: [],
      progress,
    };
  }
};
